package simulator

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"sync"
	"time"

	simontype "github.com/alibaba/open-simulator/pkg/type"
	"github.com/alibaba/open-simulator/pkg/utils"
	"github.com/olekukonko/tablewriter"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	externalclientset "k8s.io/client-go/kubernetes"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	resourcehelper "k8s.io/kubectl/pkg/util/resource"
	schedconfig "k8s.io/kubernetes/cmd/kube-scheduler/app/config"
	"k8s.io/kubernetes/pkg/scheduler"
	framework "k8s.io/kubernetes/pkg/scheduler/framework"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
)

type Simulator struct {
	// kube client
	externalclient  externalclientset.Interface
	fakeClient      externalclientset.Interface
	informerFactory informers.SharedInformerFactory

	// scheduler
	scheduler            *scheduler.Scheduler
	schedulerName        string
	defaultSchedulerConf *schedconfig.CompletedConfig

	// stopCh
	simulatorStop chan struct{}

	podcount int64

	ctx        context.Context
	cancelFunc context.CancelFunc

	// mutex
	closedMux sync.RWMutex

	status Status
}

// capture all scheduled pods with reason why the analysis could not continue
type Status struct {
	StopReason string
}

func New(externalClient externalclientset.Interface, kubeSchedulerConfig *schedconfig.CompletedConfig) (*Simulator, error) {
	var err error
	ctx, cancel := context.WithCancel(context.Background())

	// Step 1: create fake client
	fakeClient := fakeclientset.NewSimpleClientset()
	sharedInformerFactory := informers.NewSharedInformerFactory(fakeClient, 0)

	// Step 2: Create the simulator
	sim := &Simulator{
		externalclient:  externalClient,
		fakeClient:      fakeClient,
		simulatorStop:   make(chan struct{}),
		informerFactory: sharedInformerFactory,
		ctx:             ctx,
		cancelFunc:      cancel,
		schedulerName:   simontype.DefaultSchedulerName,
	}

	// Step 3: add event handler for pods
	sim.informerFactory.Core().V1().Pods().Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				if pod, ok := obj.(*corev1.Pod); ok && pod.Spec.SchedulerName == sim.schedulerName {
					return true
				}
				return false
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj interface{}) {
					if _, ok := obj.(*corev1.Pod); ok {
						// fmt.Printf("test add pod %s/%s\n", pod.Namespace, pod.Name)
					}
				},
				UpdateFunc: func(oldObj, newObj interface{}) {
					if pod, ok := newObj.(*corev1.Pod); ok {
						// fmt.Printf("test update pod %s/%s\n", pod.Namespace, pod.Name)
						sim.update(pod, sim.schedulerName)
					}
				},
			},
		},
	)

	// Step 4: create scheduler for fake cluster
	kubeSchedulerConfig.Client = fakeClient
	bindRegistry := frameworkruntime.Registry{
		simontype.SimonPluginName: func(configuration runtime.Object, f framework.Handle) (framework.Plugin, error) {
			return sim.newPlugin(simontype.DefaultSchedulerName, configuration, f)
		},
	}
	sim.scheduler, err = scheduler.New(
		sim.fakeClient,
		sim.informerFactory,
		utils.GetRecorderFactory(kubeSchedulerConfig),
		sim.ctx.Done(),
		scheduler.WithProfiles(kubeSchedulerConfig.ComponentConfig.Profiles...),
		scheduler.WithAlgorithmSource(kubeSchedulerConfig.ComponentConfig.AlgorithmSource),
		scheduler.WithPercentageOfNodesToScore(kubeSchedulerConfig.ComponentConfig.PercentageOfNodesToScore),
		scheduler.WithFrameworkOutOfTreeRegistry(bindRegistry),
		scheduler.WithPodMaxBackoffSeconds(kubeSchedulerConfig.ComponentConfig.PodMaxBackoffSeconds),
		scheduler.WithPodInitialBackoffSeconds(kubeSchedulerConfig.ComponentConfig.PodInitialBackoffSeconds),
		scheduler.WithExtenders(kubeSchedulerConfig.ComponentConfig.Extenders...),
	)
	if err != nil {
		return nil, err
	}

	return sim, nil
}

func (sim *Simulator) Run(pods []*corev1.Pod) error {
	// Step 1: start all informers.
	sim.informerFactory.Start(sim.ctx.Done())
	sim.informerFactory.WaitForCacheSync(sim.ctx.Done())

	// Step 2: run scheduler
	go func() {
		sim.scheduler.Run(sim.ctx)
	}()
	// Step 3: wait some time before at least nodes are populated
	time.Sleep(100 * time.Millisecond)

	// Step 4: create the simulated pods
	sim.podcount = int64(len(pods))
	// log.Infof("sim.podcount %v", sim.podcount)
	for _, pod := range pods {
		// log.Infof("sim pod %v on node %v", pod.Namespace+"/"+pod.Name, pod.Spec.NodeName)
		_, err := sim.fakeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
		if err != nil {
			log.Errorf("create pod error: %s", err.Error())
		}
	}

	if len(pods) != 0 {
		wait.Until(func() {
			time.Sleep(50 * time.Millisecond)
		}, time.Millisecond, sim.simulatorStop)
	}

	return nil
}

func (sim *Simulator) GetStatus() string {
	return sim.status.StopReason
}

func (sim *Simulator) Report() {
	// Step 1: report pod info
	fmt.Println("Pod Info")
	podTable := tablewriter.NewWriter(os.Stdout)
	podTable.SetHeader([]string{
		"NodeName",
		"Pod",
		"CPU Requests",
		"CPU Limits",
		"Memory Requests",
		"Memory Limits",
		"Fake Pod",
	})

	nodes, _ := sim.fakeClient.CoreV1().Nodes().List(sim.ctx, metav1.ListOptions{})
	allPods, _ := sim.fakeClient.CoreV1().Pods(corev1.NamespaceAll).List(sim.ctx, metav1.ListOptions{
		// FieldSelector not work
		// issue: https://github.com/kubernetes/client-go/issues/326#issuecomment-412993326
		// FieldSelector: "spec.nodeName=%s" + node.Name,
	})
	for _, node := range nodes.Items {
		allocatable := node.Status.Allocatable
		for _, pod := range allPods.Items {
			if pod.Spec.NodeName != node.Name {
				continue
			}
			req, limit := resourcehelper.PodRequestsAndLimits(&pod)
			cpuReq, cpuLimit, memoryReq, memoryLimit := req[corev1.ResourceCPU], limit[corev1.ResourceCPU], req[corev1.ResourceMemory], limit[corev1.ResourceMemory]
			fractionCpuReq := float64(cpuReq.MilliValue()) / float64(allocatable.Cpu().MilliValue()) * 100
			fractionCpuLimit := float64(cpuLimit.MilliValue()) / float64(allocatable.Cpu().MilliValue()) * 100
			fractionMemoryReq := float64(memoryReq.Value()) / float64(allocatable.Memory().Value()) * 100
			fractionMemoryLimit := float64(memoryLimit.Value()) / float64(allocatable.Memory().Value()) * 100
			fake := "√"
			if utils.IsFake(pod.Annotations) == false {
				fake = ""
			}
			data := []string{
				node.Name,
				fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
				fmt.Sprintf("%s(%d%%)", cpuReq.String(), int64(fractionCpuReq)),
				fmt.Sprintf("%s(%d%%)", cpuLimit.String(), int64(fractionCpuLimit)),
				fmt.Sprintf("%s(%d%%)", memoryReq.String(), int64(fractionMemoryReq)),
				fmt.Sprintf("%s(%d%%)", memoryLimit.String(), int64(fractionMemoryLimit)),
				fake,
			}
			podTable.Append(data)
		}
	}
	podTable.SetAutoMergeCellsByColumnIndex([]int{0})
	podTable.SetRowLine(true)
	podTable.SetAlignment(tablewriter.ALIGN_LEFT)
	podTable.Render() // Send output

	fmt.Println()

	// Step 2: report node info
	fmt.Println("Node Info")
	nodeTable := tablewriter.NewWriter(os.Stdout)
	nodeTable.SetHeader([]string{
		"NodeName",
		"CPU Allocatable",
		"CPU Requests",
		"CPU Limits",
		"Memory Allocatable",
		"Memory Requests",
		"Memory Limits",
		"Pod Count",
		"Fake Node",
	})

	for _, node := range nodes.Items {
		reqs, limits := utils.GetPodsTotalRequestsAndLimitsByNodeName(allPods.Items, node.Name)
		nodeCpuReq, nodeCpuLimit, nodeMemoryReq, nodeMemoryLimit, _, _ :=
			reqs[corev1.ResourceCPU], limits[corev1.ResourceCPU], reqs[corev1.ResourceMemory], limits[corev1.ResourceMemory], reqs[corev1.ResourceEphemeralStorage], limits[corev1.ResourceEphemeralStorage]
		allocatable := node.Status.Allocatable
		nodeFractionCpuReq := float64(nodeCpuReq.MilliValue()) / float64(allocatable.Cpu().MilliValue()) * 100
		nodeFractionCpuLimit := float64(nodeCpuLimit.MilliValue()) / float64(allocatable.Cpu().MilliValue()) * 100
		nodeFractionMemoryReq := float64(nodeMemoryReq.Value()) / float64(allocatable.Memory().Value()) * 100
		nodeFractionMemoryLimit := float64(nodeMemoryLimit.Value()) / float64(allocatable.Memory().Value()) * 100
		fake := "√"
		if utils.IsFake(node.Annotations) == false {
			fake = ""
		}
		data := []string{
			node.Name,
			allocatable.Cpu().String(),
			fmt.Sprintf("%s(%d%%)", nodeCpuReq.String(), int64(nodeFractionCpuReq)),
			fmt.Sprintf("%s(%d%%)", nodeCpuLimit.String(), int64(nodeFractionCpuLimit)),
			allocatable.Memory().String(),
			fmt.Sprintf("%s(%d%%)", nodeMemoryReq.String(), int64(nodeFractionMemoryReq)),
			fmt.Sprintf("%s(%d%%)", nodeMemoryLimit.String(), int64(nodeFractionMemoryLimit)),
			fmt.Sprintf("%d", utils.GetNodePodsCount(allPods, node.Name)),
			fake,
		}
		nodeTable.Append(data)
	}
	nodeTable.SetRowLine(true)
	nodeTable.SetAlignment(tablewriter.ALIGN_LEFT)
	nodeTable.Render() // Send output
}

func (sim *Simulator) CreateConfigMapAndSaveItToFile(fileName string) error {
	var resultDeployment map[string][]string = make(map[string][]string)
	var resultStatefulment map[string][]string = make(map[string][]string)

	allPods, _ := sim.fakeClient.CoreV1().Pods(corev1.NamespaceAll).List(sim.ctx, metav1.ListOptions{
		// FieldSelector not work
		// issue: https://github.com/kubernetes/client-go/issues/326#issuecomment-412993326
		// FieldSelector: "spec.nodeName=%s" + node.Name,
	})
	for _, pod := range allPods.Items {
		if pod.Annotations == nil {
			continue
		}
		var kind, workloadName, workloadNamespace string
		var exist bool
		if kind, exist = pod.Annotations[simontype.AnnoWorkloadKind]; exist != true {
			continue
		}
		if workloadName, exist = pod.Annotations[simontype.AnnoWorkloadName]; exist != true {
			continue
		}
		if workloadNamespace, exist = pod.Annotations[simontype.AnnoWorkloadNamespace]; exist != true {
			continue
		}
		switch kind {
		case simontype.WorkloadKindDeployment:
			resultDeployment[fmt.Sprintf("%s/%s", workloadNamespace, workloadName)] = append(resultDeployment[fmt.Sprintf("%s/%s", workloadNamespace, workloadName)], pod.Spec.NodeName)
		case simontype.WorkloadKindStatefulSet:
			resultStatefulment[fmt.Sprintf("%s/%s", workloadNamespace, workloadName)] = append(resultStatefulment[fmt.Sprintf("%s/%s", workloadNamespace, workloadName)], pod.Spec.NodeName)
		default:
			continue
		}
	}

	utils.AdjustWorkloads(resultDeployment)
	utils.AdjustWorkloads(resultStatefulment)

	byteDeployment, _ := json.Marshal(resultDeployment)
	byteStatefulment, _ := json.Marshal(resultStatefulment)
	var resultForCM map[string]string = make(map[string]string)
	resultForCM[simontype.WorkloadKindDeployment] = string(byteDeployment)
	resultForCM[simontype.WorkloadKindStatefulSet] = string(byteStatefulment)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      simontype.ConfigMapName,
			Namespace: simontype.ConfigMapNamespace,
		},
		Data: resultForCM,
	}

	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	codec := serializer.NewCodecFactory(scheme).LegacyCodec(corev1.SchemeGroupVersion)
	output, _ := runtime.Encode(codec, configMap)
	if err := ioutil.WriteFile(fileName, output, 0644); err != nil {
		return err
	}

	return nil
}

func (sim *Simulator) BindPodToNode(ctx context.Context, state *framework.CycleState, p *corev1.Pod, nodeName string, schedulerName string) *framework.Status {
	// fmt.Printf("bind pod %s/%s to node %s\n", p.Namespace, p.Name, nodeName)
	// Step 1: update pod info
	pod, err := sim.fakeClient.CoreV1().Pods(p.Namespace).Get(context.TODO(), p.Name, metav1.GetOptions{})
	if err != nil {
		log.Errorf("fake get error %v", err)
		return framework.NewStatus(framework.Error, fmt.Sprintf("Unable to bind: %v", err))
	}
	updatedPod := pod.DeepCopy()
	updatedPod.Spec.NodeName = nodeName
	updatedPod.Status.Phase = corev1.PodRunning

	// Step 2: update pod
	// here assuming the pod is already in the resource storage
	// so the update is needed to emit update event in case a handler is registered
	_, err = sim.fakeClient.CoreV1().Pods(pod.Namespace).Update(context.TODO(), updatedPod, metav1.UpdateOptions{})
	if err != nil {
		log.Errorf("fake update error %v", err)
		return framework.NewStatus(framework.Error, fmt.Sprintf("Unable to add new pod: %v", err))
	}

	return nil
}

func (sim *Simulator) GetNodes() []corev1.Node {
	nodes, err := sim.fakeClient.CoreV1().Nodes().List(sim.ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Printf("GetNodes failed: %s\n", err.Error())
		os.Exit(1)
	}
	return nodes.Items
}

func (sim *Simulator) Close() {
	if sim.podcount == 0 {
		sim.cancelFunc()
		close(sim.simulatorStop)
	}
}

func (sim *Simulator) AddPods(pods []*corev1.Pod) error {
	for _, pod := range pods {
		log.Infof("addpods %v", pod.Namespace+"/"+pod.Name)
		_, err := sim.fakeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
		if err != nil {
			log.Errorf("create pod error: %s", err.Error())
		}
	}
	return nil
}

func (sim *Simulator) AddNodes(nodes []*corev1.Node) error {
	for _, node := range nodes {
		_, err := sim.fakeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func (sim *Simulator) AddFakeNode(nodeCount int, node *corev1.Node) error {
	if node == nil {
		return fmt.Errorf("node is nil")
	}

	// make fake node
	for i := 0; i < nodeCount; i++ {
		// create fake node
		hostname := fmt.Sprintf("%s-%02d", simontype.FakeNodeNamePrefix, i)
		node = utils.MakeValidNodeByNode(node, hostname)
		_, err := sim.fakeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		// create daemonset pod
		daemonsets, err := sim.fakeClient.AppsV1().DaemonSets(corev1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
		for _, daemonset := range daemonsets.Items {
			pod := utils.MakeValidPodByDaemonset(&daemonset, hostname)
			_, err := sim.fakeClient.CoreV1().Pods(daemonset.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (sim *Simulator) SyncFakeCluster(needPodAndNode bool) error {
	if needPodAndNode {
		// sync nodes
		nodeItems, err := sim.externalclient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("unable to list nodes: %v", err)
		}
		for _, item := range nodeItems.Items {
			if _, err := sim.fakeClient.CoreV1().Nodes().Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("unable to copy node: %v", err)
			}
		}

		// sync pods
		podItems, err := sim.externalclient.CoreV1().Pods(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("unable to list pods: %v", err)
		}
		for _, item := range podItems.Items {
			if _, err := sim.fakeClient.CoreV1().Pods(item.Namespace).Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("unable to copy pod: %v", err)
			}
		}
	}

	// sync pdb
	pdbItems, err := sim.externalclient.PolicyV1beta1().PodDisruptionBudgets(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list PDBs: %v", err)
	}
	for _, item := range pdbItems.Items {
		if _, err := sim.fakeClient.PolicyV1beta1().PodDisruptionBudgets(item.Namespace).Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy PDB: %v", err)
		}
	}

	// sync svc
	serviceItems, err := sim.externalclient.CoreV1().Services(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list services: %v", err)
	}
	for _, item := range serviceItems.Items {
		if _, err := sim.fakeClient.CoreV1().Services(item.Namespace).Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy service: %v", err)
		}
	}

	// sync sc
	storageClassesItems, err := sim.externalclient.StorageV1().StorageClasses().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list storage classes: %v", err)
	}
	for _, item := range storageClassesItems.Items {
		if _, err := sim.fakeClient.StorageV1().StorageClasses().Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy storage class: %v", err)
		}
	}

	// sync pvc
	pvcItems, err := sim.externalclient.CoreV1().PersistentVolumeClaims(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list pvcs: %v", err)
	}
	for _, item := range pvcItems.Items {
		if _, err := sim.fakeClient.CoreV1().PersistentVolumeClaims(item.Namespace).Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy pvc: %v", err)
		}
	}

	// sync rc
	rcItems, err := sim.externalclient.CoreV1().ReplicationControllers(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list RCs: %v", err)
	}
	for _, item := range rcItems.Items {
		if _, err := sim.fakeClient.CoreV1().ReplicationControllers(item.Namespace).Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy RC: %v", err)
		}
	}

	// sync deployment
	deploymentItems, err := sim.externalclient.AppsV1().Deployments(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list deployment: %v", err)
	}
	for _, item := range deploymentItems.Items {
		if _, err := sim.fakeClient.AppsV1().Deployments(item.Namespace).Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy deployment: %v", err)
		}
	}

	// sync replicaSet
	replicaSetItems, err := sim.externalclient.AppsV1().ReplicaSets(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list replicas sets: %v", err)
	}
	for _, item := range replicaSetItems.Items {
		if _, err := sim.fakeClient.AppsV1().ReplicaSets(item.Namespace).Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy replica set: %v", err)
		}
	}

	// sync statefulSet
	statefulSetItems, err := sim.externalclient.AppsV1().StatefulSets(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list stateful sets: %v", err)
	}
	for _, item := range statefulSetItems.Items {
		if _, err := sim.fakeClient.AppsV1().StatefulSets(item.Namespace).Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy stateful set: %v", err)
		}
	}

	// sync daemonSet
	daemonSetItems, err := sim.externalclient.AppsV1().DaemonSets(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list daemon sets: %v", err)
	}
	for _, item := range daemonSetItems.Items {
		if _, err := sim.fakeClient.AppsV1().DaemonSets(item.Namespace).Create(context.TODO(), &item, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("unable to copy daemon set: %v", err)
		}
	}

	return nil
}

func (sim *Simulator) update(pod *corev1.Pod, schedulerName string) error {
	var stop bool = false
	var stopReason string
	var stopMessage string
	//log.Infof("update pod %v, pod status %v", pod.Namespace+"/"+pod.Name, pod.Status)
	for _, podCondition := range pod.Status.Conditions {
		//log.Infof("podCondition %v", podCondition)
		stop = podCondition.Type == corev1.PodScheduled && podCondition.Status == corev1.ConditionFalse && podCondition.Reason == corev1.PodReasonUnschedulable
		if stop {
			stopReason = podCondition.Reason
			stopMessage = podCondition.Message
			//fmt.Printf("stop is true: %s %s\n", stopReason, stopMessage)
			break
		}
	}
	// Only for pending pods provisioned by simon
	if stop {
		if metav1.HasAnnotation(pod.ObjectMeta, simontype.AnnoPodProvisioner) {
			sim.status.StopReason = fmt.Sprintf("pod %s/%s is failed, %d pod(s) are waited to be scheduled: %s: %s", pod.Namespace, pod.Name, sim.podcount, stopReason, stopMessage)
			// The Update function can be run more than once before any corresponding
			// scheduler is closed. The behaviour is implementation specific
			// fmt.Printf("send stop message %s/%s\n", pod.Namespace, pod.Name)
			sim.simulatorStop <- struct{}{}
			sim.Close()
		}
	} else {
		sim.podcount--
		if sim.podcount == 0 {
			sim.status.StopReason = simontype.StopReasonSuccess
			// fmt.Printf("send success message %s/%s\n", pod.Namespace, pod.Name)
			sim.simulatorStop <- struct{}{}
		}
	}

	return nil
}

func (sim *Simulator) newPlugin(schedulerName string, configuration runtime.Object, f framework.Handle) (framework.Plugin, error) {
	return &SimonPlugin{
		schedulerName: schedulerName,
		sim:           sim,
	}, nil
}
