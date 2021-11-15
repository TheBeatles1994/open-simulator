package simontype

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	SimonPluginName      = "Simon"
	OpenLocalPluginName  = "Open-Local"
	NewNodeNamePrefix    = "simon"
	DefaultSchedulerName = corev1.DefaultSchedulerName

	StopReasonSuccess   = "everything is ok"
	StopReasonDoNotStop = "do not stop"
	CreateError         = "failed to create pod"

	AnnoWorkloadKind      = "simon/workload-kind"
	AnnoWorkloadName      = "simon/workload-name"
	AnnoWorkloadNamespace = "simon/workload-namespace"
	AnnoNodeLocalStorage  = "simon/node-local-storage"
	AnnoPodLocalStorage   = "simon/pod-local-storage"

	LabelDaemonSetFromCluster = "simon/daemonset-from-cluster"
	LabelNewNode              = "simon/new-node"
	LabelNewPod               = "simon/new-pod"

	EnvMaxCPU    = "MaxCPU"
	EnvMaxMemory = "MaxMemory"
	EnvMaxVG     = "MaxVG"

	WorkloadKindDeployment  = "Deployment"
	WorkloadKindStatefulSet = "StatefulSet"
	WorkloadKindDaemonSet   = "DaemonSet"

	ConfigMapName      = "simulator-plan"
	ConfigMapNamespace = metav1.NamespaceSystem
	ConfigMapFileName  = "configmap-simon.yaml"

	NotesFileSuffix            = "NOTES.txt"
	DirectoryForChart          = "/tmp/charts"
	DefaultDirectoryPermission = 0755
)
