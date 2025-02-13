package simontype

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type AliQoSClass string

const (
	SimonPluginName      = "Simon"
	FakeNodeNamePrefix   = "simon"
	DefaultSchedulerName = corev1.DefaultSchedulerName

	StopReasonSuccess = "everything is ok"

	AnnoPodProvisioner    = "oecp.io/provisioned-by"
	AnnoFake              = "oecp.io/fake"
	AnnoWorkloadKind      = "oecp.io/workload-kind"
	AnnoWorkloadName      = "oecp.io/workload-name"
	AnnoWorkloadNamespace = "oecp.io/workload-namespace"

	WorkloadKindDeployment  = "Deployment"
	WorkloadKindStatefulSet = "StatefulSet"
	WorkloadKindDaemonSet   = "DaemonSet"

	ConfigMapName      = "simulator-plan"
	ConfigMapNamespace = metav1.NamespaceSystem
	ConfigMapFileName  = "configmap-simon.yaml"
)
