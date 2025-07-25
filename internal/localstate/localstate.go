package localstate

import (
	"context"

	alertv1 "github.com/humanlogio/api/go/svc/alert/v1"
	dashboardv1 "github.com/humanlogio/api/go/svc/dashboard/v1"
	stackv1 "github.com/humanlogio/api/go/svc/stack/v1"
	"github.com/humanlogio/humanlog/internal/localalert"
)

type DB interface {
	CreateStack(context.Context, *stackv1.CreateStackRequest) (*stackv1.CreateStackResponse, error)
	GetStack(context.Context, *stackv1.GetStackRequest) (*stackv1.GetStackResponse, error)
	UpdateStack(context.Context, *stackv1.UpdateStackRequest) (*stackv1.UpdateStackResponse, error)
	DeleteStack(context.Context, *stackv1.DeleteStackRequest) (*stackv1.DeleteStackResponse, error)
	ListStack(context.Context, *stackv1.ListStackRequest) (*stackv1.ListStackResponse, error)

	CreateDashboard(ctx context.Context, req *dashboardv1.CreateDashboardRequest) (*dashboardv1.CreateDashboardResponse, error)
	GetDashboard(ctx context.Context, req *dashboardv1.GetDashboardRequest) (*dashboardv1.GetDashboardResponse, error)
	UpdateDashboard(ctx context.Context, req *dashboardv1.UpdateDashboardRequest) (*dashboardv1.UpdateDashboardResponse, error)
	DeleteDashboard(ctx context.Context, req *dashboardv1.DeleteDashboardRequest) (*dashboardv1.DeleteDashboardResponse, error)
	ListDashboard(ctx context.Context, req *dashboardv1.ListDashboardRequest) (*dashboardv1.ListDashboardResponse, error)

	CreateAlertGroup(ctx context.Context, req *alertv1.CreateAlertGroupRequest) (*alertv1.CreateAlertGroupResponse, error)
	GetAlertGroup(ctx context.Context, req *alertv1.GetAlertGroupRequest) (*alertv1.GetAlertGroupResponse, error)
	UpdateAlertGroup(ctx context.Context, req *alertv1.UpdateAlertGroupRequest) (*alertv1.UpdateAlertGroupResponse, error)
	DeleteAlertGroup(ctx context.Context, req *alertv1.DeleteAlertGroupRequest) (*alertv1.DeleteAlertGroupResponse, error)
	ListAlertGroup(ctx context.Context, req *alertv1.ListAlertGroupRequest) (*alertv1.ListAlertGroupResponse, error)

	CreateAlertRule(ctx context.Context, req *alertv1.CreateAlertRuleRequest) (*alertv1.CreateAlertRuleResponse, error)
	GetAlertRule(ctx context.Context, req *alertv1.GetAlertRuleRequest) (*alertv1.GetAlertRuleResponse, error)
	UpdateAlertRule(ctx context.Context, req *alertv1.UpdateAlertRuleRequest) (*alertv1.UpdateAlertRuleResponse, error)
	DeleteAlertRule(ctx context.Context, req *alertv1.DeleteAlertRuleRequest) (*alertv1.DeleteAlertRuleResponse, error)
	ListAlertRule(ctx context.Context, req *alertv1.ListAlertRuleRequest) (*alertv1.ListAlertRuleResponse, error)

	AlertStateStorage() localalert.AlertStorage
}
