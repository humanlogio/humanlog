package localstate

import (
	"context"

	alertv1 "github.com/humanlogio/api/go/svc/alert/v1"
	dashboardv1 "github.com/humanlogio/api/go/svc/dashboard/v1"
	typesv1 "github.com/humanlogio/api/go/types/v1"
)

type DB interface {
	CreateDashboard(ctx context.Context, req *dashboardv1.CreateDashboardRequest) (*typesv1.Dashboard, error)
	GetDashboard(ctx context.Context, id string) (*typesv1.Dashboard, error)
	UpdateDashboard(ctx context.Context, id string, mutations []*dashboardv1.UpdateDashboardRequest_Mutation) (*typesv1.Dashboard, error)
	DeleteDashboard(ctx context.Context, id string) error
	ListDashboard(ctx context.Context, cursor *typesv1.Cursor, limit int32) ([]*typesv1.Dashboard, *typesv1.Cursor, error)

	CreateAlertRule(ctx context.Context, req *alertv1.CreateAlertRuleRequest) (*alertv1.CreateAlertRuleResponse, error)
	GetAlertRule(ctx context.Context, req *alertv1.GetAlertRuleRequest) (*alertv1.GetAlertRuleResponse, error)
	UpdateAlertRule(ctx context.Context, req *alertv1.UpdateAlertRuleRequest) (*alertv1.UpdateAlertRuleResponse, error)
	DeleteAlertRule(ctx context.Context, req *alertv1.DeleteAlertRuleRequest) (*alertv1.DeleteAlertRuleResponse, error)
	ListAlertRule(ctx context.Context, req *alertv1.ListAlertRuleRequest) (*alertv1.ListAlertRuleResponse, error)
}
