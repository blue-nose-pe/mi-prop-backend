// Package proxy contiene los handlers HTTP que traducen REST → gRPC.
//
// Cada microservicio expone su contrato gRPC; el gateway dialea cada
// uno con un client persistente (gRPC keepalive). La traducción
// request/response se hace explícita por handler — más boilerplate
// que grpc-gateway autogenerado, pero da control fino sobre el shape
// del JSON público y evita modificar los .proto por requisitos REST.
package proxy

import (
	"time"

	usersgrpcpb "users_service/proto/gen"
	examsgrpcpb "exams_service/proto/gen"
	keysgrpcpb "keys_service/proto/gen"
	hubspotgrpcpb "hubspot_service/proto/gen"
	satisfactiongrpcpb "satisfaction_service/proto/gen"
	analyticsgrpcpb "analytics_service/proto/gen"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// Clients agrupa los clientes gRPC a todos los upstreams.
type Clients struct {
	Users        usersgrpcpb.UserServiceClient
	Auth         usersgrpcpb.AuthServiceClient
	Schools      usersgrpcpb.SchoolServiceClient
	Leads        usersgrpcpb.LeadServiceClient
	Visitas      usersgrpcpb.VisitaServiceClient
	PermGroups   usersgrpcpb.PermissionGroupServiceClient
	Exams        examsgrpcpb.ExamServiceClient
	Questions    examsgrpcpb.QuestionServiceClient
	ExamQs       examsgrpcpb.ExamQuestionServiceClient
	Attempts     examsgrpcpb.AttemptServiceClient
	Keys         keysgrpcpb.KeyServiceClient
	Hubspot      hubspotgrpcpb.HubspotServiceClient
	Surveys      satisfactiongrpcpb.SurveyServiceClient
	Responses    satisfactiongrpcpb.ResponseServiceClient
	Analytics    analyticsgrpcpb.AnalyticsServiceClient

	conns []*grpc.ClientConn
}

// Addrs centraliza las direcciones (todas se inyectan desde config).
type Addrs struct {
	Users        string
	Exams        string
	Keys         string
	Hubspot      string
	Satisfaction string
	Analytics    string
}

// Dial abre conexiones gRPC a todos los upstreams. El gateway corre dentro
// del cluster AKS → comunicación interna sin TLS (insecure). El borde TLS
// lo termina el Ingress NGINX.
func Dial(a Addrs) (*Clients, error) {
	keepaliveOpts := grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:                30 * time.Second,
		Timeout:             10 * time.Second,
		PermitWithoutStream: true,
	})
	creds := grpc.WithTransportCredentials(insecure.NewCredentials())

	dial := func(addr string) (*grpc.ClientConn, error) {
		return grpc.NewClient(addr, creds, keepaliveOpts)
	}

	usersConn, err := dial(a.Users)
	if err != nil {
		return nil, err
	}
	examsConn, err := dial(a.Exams)
	if err != nil {
		return nil, err
	}
	keysConn, err := dial(a.Keys)
	if err != nil {
		return nil, err
	}
	hsConn, err := dial(a.Hubspot)
	if err != nil {
		return nil, err
	}
	satConn, err := dial(a.Satisfaction)
	if err != nil {
		return nil, err
	}
	anConn, err := dial(a.Analytics)
	if err != nil {
		return nil, err
	}

	return &Clients{
		Users:      usersgrpcpb.NewUserServiceClient(usersConn),
		Auth:       usersgrpcpb.NewAuthServiceClient(usersConn),
		Schools:    usersgrpcpb.NewSchoolServiceClient(usersConn),
		Leads:      usersgrpcpb.NewLeadServiceClient(usersConn),
		Visitas:    usersgrpcpb.NewVisitaServiceClient(usersConn),
		PermGroups: usersgrpcpb.NewPermissionGroupServiceClient(usersConn),
		Exams:     examsgrpcpb.NewExamServiceClient(examsConn),
		Questions: examsgrpcpb.NewQuestionServiceClient(examsConn),
		ExamQs:    examsgrpcpb.NewExamQuestionServiceClient(examsConn),
		Attempts:  examsgrpcpb.NewAttemptServiceClient(examsConn),
		Keys:      keysgrpcpb.NewKeyServiceClient(keysConn),
		Hubspot:   hubspotgrpcpb.NewHubspotServiceClient(hsConn),
		Surveys:   satisfactiongrpcpb.NewSurveyServiceClient(satConn),
		Responses: satisfactiongrpcpb.NewResponseServiceClient(satConn),
		Analytics: analyticsgrpcpb.NewAnalyticsServiceClient(anConn),
		conns:     []*grpc.ClientConn{usersConn, examsConn, keysConn, hsConn, satConn, anConn},
	}, nil
}

func (c *Clients) Close() {
	for _, conn := range c.conns {
		_ = conn.Close()
	}
}
