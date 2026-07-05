package grpchandler

import (
	"context"
	"strconv"
	"time"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"
	"users_service/internal/shared/apperr"
	"users_service/internal/shared/jwtmw"
	pb "users_service/proto/gen"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// SchoolHandler implementa pb.SchoolServiceServer. Lectura de schools +
// assignment de asesores via AssignmentRepository (SCD-2).
type SchoolHandler struct {
	pb.UnimplementedSchoolServiceServer
	repo        ports.SchoolRepository
	assignments ports.AssignmentRepository
}

func NewSchoolHandler(repo ports.SchoolRepository, assignments ports.AssignmentRepository) *SchoolHandler {
	return &SchoolHandler{repo: repo, assignments: assignments}
}

func (h *SchoolHandler) GetSchool(ctx context.Context, req *pb.GetSchoolRequest) (*pb.SchoolResponse, error) {
	if req.GetId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_ID", "id is required", "id"))
	}
	s, err := h.repo.FindByID(ctx, domain.SchoolID(req.GetId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.SchoolResponse{School: schoolToProto(s)}, nil
}

func (h *SchoolHandler) CreateSchool(ctx context.Context, req *pb.CreateSchoolRequest) (*pb.SchoolResponse, error) {
	if req.GetName() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_NAME", "name is required", "name"))
	}
	if req.GetUserId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_USER_ID", "user_id (coordinador owner) is required", "user_id"))
	}
	if cat := req.GetCategory(); cat != "" && !isValidSchoolCategory(cat) {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("INVALID_CATEGORY", "category must be one of: A1, A2, B1, B2, C, OP, OR (or legacy A+/A/B/D)", "category"))
	}
	if pen := req.GetPenetration(); pen != "" && !isValidPenetration(pen) {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("INVALID_PENETRATION", "penetration must be 1-100 (or legacy Alta/Media/Baja)", "penetration"))
	}
	s := &domain.School{
		Name:            req.GetName(),
		UserID:          domain.UserID(req.GetUserId()),
		City:            req.GetCity(),
		Category:        req.GetCategory(),
		Code:            req.GetCode(),
		Penetration:     req.GetPenetration(),
		Email:           req.GetEmail(),
		Phone:           req.GetPhone(),
		Ruc:             req.GetRuc(),
		Poblacion:       req.GetPoblacion(),
		PersonalACargo:  req.GetPersonalACargo(),
		Active:          true,
		HubspotRecordID: req.GetHubspotRecordId(),
		CreatedAt:       time.Now(),
	}
	id, err := h.repo.Create(ctx, s)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	s.ID = id
	return &pb.SchoolResponse{School: schoolToProto(s)}, nil
}

func (h *SchoolHandler) UpdateSchool(ctx context.Context, req *pb.UpdateSchoolRequest) (*pb.SchoolResponse, error) {
	if req.GetId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_ID", "id is required", "id"))
	}
	if cat := req.GetCategory(); cat != "" && cat != "-" && !isValidSchoolCategory(cat) {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("INVALID_CATEGORY", "category must be one of: A1, A2, B1, B2, C, OP, OR (or legacy A+/A/B/D)", "category"))
	}
	if pen := req.GetPenetration(); pen != "" && pen != "-" && !isValidPenetration(pen) {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("INVALID_PENETRATION", "penetration must be 1-100 (or legacy Alta/Media/Baja)", "penetration"))
	}
	s := &domain.School{
		ID:              domain.SchoolID(req.GetId()),
		Name:            req.GetName(),
		UserID:          domain.UserID(req.GetUserId()),
		HubspotRecordID: req.GetHubspotRecordId(),
		City:            req.GetCity(),
		Category:        req.GetCategory(),
		Code:            req.GetCode(),
		Penetration:     req.GetPenetration(),
		Email:           req.GetEmail(),
		Phone:           req.GetPhone(),
		Ruc:             req.GetRuc(),
		Poblacion:       req.GetPoblacion(),
		PersonalACargo:  req.GetPersonalACargo(),
		ActiveRaw:       req.GetActive(), // C1: ""=no tocar, "true"/"false"
	}
	if err := h.repo.Update(ctx, s); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	updated, err := h.repo.FindByID(ctx, s.ID)
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.SchoolResponse{School: schoolToProto(updated)}, nil
}

func (h *SchoolHandler) ListSchoolsByAsesor(ctx context.Context, req *pb.ListSchoolsByAsesorRequest) (*pb.ListSchoolsResponse, error) {
	if req.GetAsesorId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_ASESOR_ID", "asesor_id is required", "asesor_id"))
	}
	items, err := h.repo.ListByAsesor(ctx, domain.UserID(req.GetAsesorId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	out := make([]*pb.School, 0, len(items))
	for i := range items {
		out = append(out, schoolToProto(&items[i]))
	}
	return &pb.ListSchoolsResponse{Items: out, Total: uint32(len(out))}, nil
}

// AssignAsesor: registra una asignacion SCD-2 kind=asesor_de_colegio donde
// source=asesor (user a asignar) y target=coordinador del colegio
// (school.user_id). Cierra la vigente previa si existe.
func (h *SchoolHandler) AssignAsesor(ctx context.Context, req *pb.AssignAsesorRequest) (*pb.EmptyResponse, error) {
	// Defense-in-depth: SOLO superadmin puede reasignar asesores. Patron
	// identico a ResetPassword. Sin esto, el grupo asesor_permissions
	// incluye db_users.school.write — entonces un asesor podia "robarse"
	// colegios de otros asesores. Bug encontrado en auditoria 2026-05-28.
	c := jwtmw.FromContext(ctx)
	isSuper := false
	if c != nil {
		for _, r := range c.Roles {
			if r == "superadmin" {
				isSuper = true
				break
			}
		}
	}
	if !isSuper {
		return nil, apperr.ToGRPC(ctx, apperr.NewPermissionDenied("ONLY_SUPERADMIN", "only superadmin can reassign asesores"))
	}
	if req.GetSchoolId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_SCHOOL_ID", "school_id is required", "school_id"))
	}
	if req.GetUserId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_USER_ID", "user_id (asesor) is required", "user_id"))
	}
	s, err := h.repo.FindByID(ctx, domain.SchoolID(req.GetSchoolId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	if s == nil || s.UserID == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("SCHOOL_NO_COORDINATOR", "school has no coordinator user_id", "school_id"))
	}
	// source = asesor (nuevo), target = coordinador del colegio.
	// by = asesor mismo por simplicidad — el audit log de assignment recoge created_by.
	if err := h.assignments.Reassign(
		ctx,
		ports.AssignmentAsesorDeColegio,
		domain.UserID(req.GetUserId()),
		s.UserID,
		domain.UserID(req.GetUserId()),
	); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

// ctxIsSuperadmin: SOLO superadmin gestiona asignaciones (mismo criterio que
// AssignAsesor — el grupo asesor incluye school.write y no debe poder tocar
// coordinadores de colegios ajenos).
func ctxIsSuperadmin(ctx context.Context) bool {
	c := jwtmw.FromContext(ctx)
	if c == nil {
		return false
	}
	for _, r := range c.Roles {
		if r == "superadmin" {
			return true
		}
	}
	return false
}

// ctxUserID: user_id del caller (JWT sub). "" si no hay claims.
func ctxUserID(ctx context.Context) string {
	c := jwtmw.FromContext(ctx)
	if c == nil {
		return ""
	}
	return c.Subject
}

// canManageCoordinador: quién puede asignar/revocar coordinadores de un colegio.
// El PERMISO db_users.coordinador.write ya lo exige permmw (ver PermissionMap);
// acá aplicamos el SCOPE: superadmin en cualquier colegio; un asesor SOLO en los
// suyos (para que no toque coordinadores de colegios ajenos). Cliente L53.
func (h *SchoolHandler) canManageCoordinador(ctx context.Context, schoolID domain.SchoolID) bool {
	if ctxIsSuperadmin(ctx) {
		return true
	}
	caller := ctxUserID(ctx)
	if caller == "" {
		return false
	}
	mine, err := h.repo.ListByAsesor(ctx, domain.UserID(caller))
	if err != nil {
		return false
	}
	for i := range mine {
		if mine[i].ID == schoolID {
			// Colegio DESACTIVADO ("en reserva"): el asesor no gestiona sus
			// coordinadores hasta reactivarlo (el superadmin sí, por el early
			// return de arriba). Cierra assign y revoke a la vez.
			return mine[i].Active
		}
	}
	return false
}

// AssignCoordinador AGREGA un coordinador al colegio (many-to-many: no cierra
// los otros). assignment kind=coordinador_de_colegio, source=coordinador,
// target=school.user_id (ancla). Idempotente para el mismo coordinador.
func (h *SchoolHandler) AssignCoordinador(ctx context.Context, req *pb.AssignCoordinadorRequest) (*pb.EmptyResponse, error) {
	if !h.canManageCoordinador(ctx, domain.SchoolID(req.GetSchoolId())) {
		return nil, apperr.ToGRPC(ctx, apperr.NewPermissionDenied("COORDINADOR_SCOPE", "solo el asesor del colegio (o superadmin) puede gestionar sus coordinadores"))
	}
	if req.GetSchoolId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_SCHOOL_ID", "school_id is required", "school_id"))
	}
	if req.GetUserId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_USER_ID", "user_id (coordinador) is required", "user_id"))
	}
	s, err := h.repo.FindByID(ctx, domain.SchoolID(req.GetSchoolId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	if s == nil || s.UserID == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("SCHOOL_NO_ANCHOR", "el colegio no tiene coordinador principal; contacta a soporte", "school_id"))
	}
	if err := h.assignments.AddSource(
		ctx,
		ports.AssignmentCoordinadorDeColegio,
		domain.UserID(req.GetUserId()),
		s.UserID,
		domain.UserID(req.GetUserId()),
	); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

// RevokeCoordinador quita un coordinador del colegio (cierra su assignment vigente).
func (h *SchoolHandler) RevokeCoordinador(ctx context.Context, req *pb.RevokeCoordinadorRequest) (*pb.EmptyResponse, error) {
	if !h.canManageCoordinador(ctx, domain.SchoolID(req.GetSchoolId())) {
		return nil, apperr.ToGRPC(ctx, apperr.NewPermissionDenied("COORDINADOR_SCOPE", "solo el asesor del colegio (o superadmin) puede gestionar sus coordinadores"))
	}
	if req.GetSchoolId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_SCHOOL_ID", "school_id is required", "school_id"))
	}
	if req.GetUserId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_USER_ID", "user_id (coordinador) is required", "user_id"))
	}
	s, err := h.repo.FindByID(ctx, domain.SchoolID(req.GetSchoolId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	if s == nil || s.UserID == "" {
		return &pb.EmptyResponse{}, nil // nada que revocar
	}
	if err := h.assignments.RevokeSource(
		ctx,
		ports.AssignmentCoordinadorDeColegio,
		domain.UserID(req.GetUserId()),
		s.UserID,
		domain.UserID(req.GetUserId()),
	); err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	return &pb.EmptyResponse{}, nil
}

// ListCoordinadoresBySchool lista los coordinadores vigentes del colegio.
func (h *SchoolHandler) ListCoordinadoresBySchool(ctx context.Context, req *pb.ListCoordinadoresBySchoolRequest) (*pb.ListCoordinadoresResponse, error) {
	if req.GetSchoolId() == "" {
		return nil, apperr.ToGRPC(ctx, apperr.NewValidation("MISSING_SCHOOL_ID", "school_id is required", "school_id"))
	}
	users, err := h.repo.ListCoordinadores(ctx, domain.SchoolID(req.GetSchoolId()))
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	items := make([]*pb.CoordinadorInfo, 0, len(users))
	for i := range users {
		items = append(items, &pb.CoordinadorInfo{
			Id:        string(users[i].ID),
			FirstName: users[i].FirstName,
			LastName:  users[i].LastName,
			Email:     string(users[i].Email),
		})
	}
	return &pb.ListCoordinadoresResponse{Items: items}, nil
}

func (h *SchoolHandler) ListSchools(ctx context.Context, req *pb.ListSchoolsRequest) (*pb.ListSchoolsResponse, error) {
	items, total, err := h.repo.List(ctx, ports.ListSchoolsInput{
		Search:     req.GetSearch(),
		Limit:      req.GetLimit(),
		Offset:     req.GetOffset(),
		ActiveOnly: req.GetActiveOnly(),
	})
	if err != nil {
		return nil, apperr.ToGRPC(ctx, err)
	}
	out := make([]*pb.School, 0, len(items))
	for i := range items {
		out = append(out, schoolToProto(&items[i]))
	}
	return &pb.ListSchoolsResponse{Items: out, Total: total}, nil
}

func schoolToProto(s *domain.School) *pb.School {
	if s == nil {
		return nil
	}
	out := &pb.School{
		Id:              string(s.ID),
		UserId:          string(s.UserID),
		Name:            s.Name,
		City:            s.City,
		Category:        s.Category,
		Code:            s.Code,
		Penetration:     s.Penetration,
		Email:           s.Email,
		Phone:           s.Phone,
		Ruc:             s.Ruc,
		Poblacion:       s.Poblacion,
		PersonalACargo:  s.PersonalACargo,
		AsesorUserId:         s.AsesorUserID,
		AsesorName:           s.AsesorName,
		CoordinadoresNombres: s.CoordinadoresNombres,
		Active:               s.Active,
		HubspotRecordId: s.HubspotRecordID,
		IntId:           s.IntID,
		CreatedAt:       timestamppb.New(s.CreatedAt),
	}
	if s.UpdatedAt != nil {
		out.UpdatedAt = timestamppb.New(*s.UpdatedAt)
	}
	return out
}

// isValidSchoolCategory acepta los valores que el constraint check de la
// migracion 023 permite (catalogo cliente 2026 + legacy preservado).
// Sincronizar si cambian alli.
func isValidSchoolCategory(c string) bool {
	switch c {
	// Catalogo nuevo (Segmento) — cliente 2026.
	case "A1", "A2", "B1", "B2", "C", "OP", "OR":
		return true
	// Catalogo legacy preservado durante migracion gradual.
	case "A+", "A", "B", "D":
		return true
	}
	return false
}

// isValidPenetration acepta la penetración NUMÉRICA 1-100 (cliente C2: campo
// tipeable, como prod) o los valores legacy Alta/Media/Baja (data pre-2026,
// para no romper updates de colegios viejos).
func isValidPenetration(p string) bool {
	switch p {
	case "Alta", "Media", "Baja":
		return true
	}
	if n, err := strconv.Atoi(p); err == nil && n >= 1 && n <= 100 {
		return true
	}
	return false
}
