// Carga masiva de usuarios — endpoint POST /api/users/bulk.
//
// Acepta dos formatos según el header Content-Type:
//
//   1) application/json — body con shape:
//      {
//        "permission_group_id": 1,                  // opcional
//        "items": [
//          {"email":"a@x.pe","password":"...","first_name":"A","last_name":"B",
//           "document_number":"12345678","school_id":""},
//          ...
//        ]
//      }
//
//   2) text/csv — primera fila de header con columnas:
//      email,password,first_name,last_name,document_number,school_id
//      ...filas con los valores correspondientes...
//      (permission_group_id se pasa por query param ?permission_group_id=1)
//
// El gateway itera fila por fila llamando a UsersService.CreateUser; si el
// caller pidió asignar un grupo, también llama AssignPermissionGroup. La
// respuesta resume created/errors para que el frontend muestre el detalle:
//
//   {
//     "created": [{"index": 0, "id": "...", "email": "..."}, ...],
//     "errors":  [{"index": 3, "email": "...", "code": "EMAIL_TAKEN", "message": "..."}]
//   }
//
// El sync a HubSpot se dispara automáticamente desde users-service por cada
// CreateUser exitoso (no hace falta llamarlo aparte).
package proxy

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	usersgrpcpb "users_service/proto/gen"

	"google.golang.org/grpc/status"
)

type bulkUserItem struct {
	Email          string `json:"email"`
	Password       string `json:"password"`
	FirstName      string `json:"first_name"`
	LastName       string `json:"last_name"`
	DocumentNumber string `json:"document_number"`
	SchoolID       string `json:"school_id"`
}

type bulkCreateUsersRequest struct {
	PermissionGroupID uint32         `json:"permission_group_id"`
	Items             []bulkUserItem `json:"items"`
}

type bulkCreated struct {
	Index int    `json:"index"`
	ID    string `json:"id"`
	Email string `json:"email"`
}

type bulkError struct {
	Index   int    `json:"index"`
	Email   string `json:"email"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// bulkCreateUsers procesa el array de items y reporta resultado por cada uno.
// La operación NO es transaccional: si la fila 5 falla, las primeras 4 quedan
// creadas. El frontend ve `created` y `errors` y muestra el detalle al admin.
func (p *Proxy) bulkCreateUsers(w http.ResponseWriter, r *http.Request) {
	in, err := parseBulkBody(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BAD_BODY", Message: err.Error()})
		return
	}
	if len(in.Items) == 0 {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "EMPTY_BULK", Message: "items must contain at least one entry"})
		return
	}
	if len(in.Items) > 1000 {
		writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Code: "BULK_TOO_LARGE", Message: "items cannot exceed 1000 entries per request"})
		return
	}

	created := make([]bulkCreated, 0, len(in.Items))
	failed := make([]bulkError, 0)

	for i, it := range in.Items {
		resp, err := p.cli.Users.CreateUser(r.Context(), &usersgrpcpb.CreateUserRequest{
			Email:          it.Email,
			Password:       it.Password,
			FirstName:      it.FirstName,
			LastName:       it.LastName,
			DocumentNumber: it.DocumentNumber,
			SchoolId:       it.SchoolID,
		})
		if err != nil {
			failed = append(failed, bulkError{Index: i, Email: it.Email, Code: grpcCode(err), Message: grpcMessage(err)})
			continue
		}
		userID := resp.GetUser().GetId()
		created = append(created, bulkCreated{Index: i, ID: userID, Email: it.Email})

		if in.PermissionGroupID > 0 {
			_, err := p.cli.Users.AssignPermissionGroup(r.Context(), &usersgrpcpb.AssignGroupRequest{
				UserId:            userID,
				PermissionGroupId: in.PermissionGroupID,
			})
			if err != nil {
				failed = append(failed, bulkError{Index: i, Email: it.Email, Code: "ASSIGN_GROUP_FAILED", Message: grpcMessage(err)})
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"created": created,
		"errors":  failed,
		"summary": map[string]int{
			"total":   len(in.Items),
			"created": len(created),
			"failed":  len(failed),
		},
	})
}

// parseBulkBody soporta JSON y CSV según Content-Type. Para CSV admite
// el permission_group_id como query param.
func parseBulkBody(r *http.Request) (*bulkCreateUsersRequest, error) {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	switch ct {
	case "application/json", "":
		var in bulkCreateUsersRequest
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		return &in, nil
	case "text/csv":
		items, err := parseBulkCSV(r.Body)
		if err != nil {
			return nil, err
		}
		var groupID uint32
		if v := r.URL.Query().Get("permission_group_id"); v != "" {
			n, err := strconv.ParseUint(v, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("permission_group_id query param must be a positive integer")
			}
			groupID = uint32(n)
		}
		return &bulkCreateUsersRequest{PermissionGroupID: groupID, Items: items}, nil
	default:
		return nil, fmt.Errorf("unsupported Content-Type %q (use application/json or text/csv)", ct)
	}
}

// parseBulkCSV lee un CSV con header obligatorio. Las columnas requeridas son
// email y password; las demás son opcionales y pueden estar en cualquier orden
// o ausentes.
func parseBulkCSV(body io.Reader) ([]bulkUserItem, error) {
	rd := csv.NewReader(body)
	rd.TrimLeadingSpace = true
	header, err := rd.Read()
	if err != nil {
		return nil, fmt.Errorf("CSV header missing or malformed: %w", err)
	}
	idx := map[string]int{}
	for i, col := range header {
		idx[strings.ToLower(strings.TrimSpace(col))] = i
	}
	if _, ok := idx["email"]; !ok {
		return nil, errors.New("CSV header must include 'email' column")
	}
	if _, ok := idx["password"]; !ok {
		return nil, errors.New("CSV header must include 'password' column")
	}

	get := func(row []string, col string) string {
		if i, ok := idx[col]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}

	items := make([]bulkUserItem, 0)
	for line := 2; ; line++ {
		row, err := rd.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("CSV parse error at line %d: %w", line, err)
		}
		items = append(items, bulkUserItem{
			Email:          get(row, "email"),
			Password:       get(row, "password"),
			FirstName:      get(row, "first_name"),
			LastName:       get(row, "last_name"),
			DocumentNumber: get(row, "document_number"),
			SchoolID:       get(row, "school_id"),
		})
	}
	return items, nil
}

// grpcCode mapea errores gRPC a un string corto para el reporte. Si el
// error no es gRPC, devuelve "INTERNAL".
func grpcCode(err error) string {
	if s, ok := status.FromError(err); ok {
		return s.Code().String()
	}
	return "INTERNAL"
}

func grpcMessage(err error) string {
	if s, ok := status.FromError(err); ok {
		return s.Message()
	}
	return err.Error()
}
