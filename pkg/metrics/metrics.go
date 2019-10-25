package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	MetricsRBACSyncConfig        = "RBACSyncConfig"
	MetricsClusterRBACSyncConfig = "ClusterRBACSyncConfig"

	MetricsRoleBinding        = "RoleBinding"
	MetricsClusterRoleBinding = "ClusterRoleBinding"
)

var (
	// Metrics for Controller
	RBACSyncConfigStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "rbacsync_config_status",
		Help: "The number of RBACSyncConfigs and RBACSyncClusterConfigs and the status of the processed config",
	}, []string{"kind", "status"})
	RBACSyncBindingStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "rbacsync_binding_status",
		Help: "The number of RoleBindings and ClusterRoleBIndings configured by the controller and their statuses",
	}, []string{"kind", "status"})

	// Metrics for Mapper/GSuite
	RBACSyncGsuiteClientCreationStatus = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rbacsync_gsuite_client_creation_status",
		Help: "Total number of the status of gsuite client creations",
	})
	RBACSyncGsuiteMembersStatus = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "rbacsync_gsuite_members_status",
		Help: "Total number of the status of calls to gsuite with labels for state",
	}, []string{"status"})
	RBACSyncGsuiteMembersLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "rbacsync_gsuite_members_latency_duration_seconds",
		Help: "The amount of time the calls to gsuite for group memberships",
	}, []string{"status"})
)
