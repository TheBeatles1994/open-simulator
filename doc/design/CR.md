# CustomResource资源规范

- [CustomResource资源规范](#customresource资源规范)
  - [背景](#背景)
  - [规范](#规范)
  - [Example](#example)

## 背景

Open-Simulator 支持的 K8s [Workload](https://kubernetes.io/zh/docs/concepts/workloads/) 类型有三种，即 Deployment、Statefulset、Daemonset。实际应用部署时，除了常见的 Workload 类型以外，还会有[定制资源（Custom Resource）](https://kubernetes.io/zh/docs/concepts/extend-kubernetes/api-extension/custom-resources/)，应用的 Operator 会根据定制资源生成 K8s 可识别的 Workload 资源。因 Open-Simulator 模拟集群并没有运行真实集群，故无法预测 Operator 的行为。为此，Open-Simulator 针对定制资源设计一套规范。

## 规范

Operator 会根据 CR 生成一个或多个 Workload，Open-Simulator 要求应用提供方为每个 Workload 提供一个规范，这样 Open-Simulator 可根据规范模拟出相关资源，进行资源规划。

规范如下：

```yaml
# 生成的 Workload 名称
name: [workload 名称]
# 生成的 Workload 命名空间
namespace: [workload 命名空间]
# 副本数
replicas: [副本数]
# 应用亲和性规则，与 K8s API 使用一致
affinity:
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
      - matchExpressions:
        - key: node-role.kubernetes.io/master
          operator: Exists
# 容忍度，与 K8s API 使用一致
tolerations:
- effect: NoSchedule
  key: node-role.kubernetes.io/master
  operator: Exists
# 资源需求。若存在多个容器，需填写 resources 总和。与 K8s API 使用一致
resources:
  limits:
    cpu: 2
    memory: 1024Mi
  requests:
    cpu: 1
    memory: 512Mi
# 存储类模板。与 K8s API 使用一致
volumeClaimTemplates:
- apiVersion: v1
  kind: PersistentVolumeClaim
  metadata:
    name: [pvc 名称]
  spec:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 1Gi # 容量大小
    volumeMode: Filesystem
```

## Example

下面以 [Prometheus](https://github.com/prometheus-operator/prometheus-operator) 资源为例，CR如下：

```yaml
apiVersion: monitoring.coreos.com/v1
kind: Prometheus
metadata:
  name: kube-prometheus-stack-prometheus
  namespace: monitoring
spec:
  affinity:
    podAntiAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
      - podAffinityTerm:
          labelSelector:
            matchLabels:
              app: prometheus
              prometheus: kube-prometheus-stack-prometheus
          topologyKey: kubernetes.io/hostname
        weight: 100
  alerting:
    alertmanagers:
    - apiVersion: v2
      name: kube-prometheus-stack-alertmanager
      namespace: acs-system
      pathPrefix: /
      port: web
  enableAdminAPI: false
  externalUrl: http://kube-prometheus-stack-prometheus.monitoring:9090
  image: quay.io/prometheus/prometheus:v2.26.0
  listenLocal: false
  logFormat: logfmt
  logLevel: info
  paused: false
  portName: web
  replicas: 2
  resources:
    limits:
      memory: 6Gi
    requests:
      memory: 2Gi
  retention: 15d
  routePrefix: /
  securityContext:
    fsGroup: 2000
    runAsGroup: 2000
    runAsNonRoot: true
    runAsUser: 1000
  serviceAccountName: kube-prometheus-stack-prometheus
  storage:
    volumeClaimTemplate:
      spec:
        accessModes:
        - ReadWriteOnce
        resources:
          requests:
            storage: 50Gi
  version: v2.26.0
```

上面的 CR 会生成两个 Statefulset，分别是 alertmanager-kube-prometheus-stack-alertmanager 和 prometheus-kube-prometheus-stack-prometheus。对于 Open-Simulator 而言，需要应用方按照规范提供如下 Yaml 文件，Open-Simulator 会根据这些信息进行资源规划。

由于有两个 Statefulset，用户需要提供两个规范文件。其中 alertmanager-kube-prometheus-stack-alertmanager 的规范如下：

```yaml
# 以 alertmanager-kube-prometheus-stack-alertmanager 为例。

# 生成的 Workload 名称
name: alertmanager-kube-prometheus-stack-alertmanager
# 生成的 Workload 命名空间
namespace: monitoring
# 副本数
replicas: 2
# 应用亲和性规则
affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
    - podAffinityTerm:
        labelSelector:
        matchLabels:
            alertmanager: kube-prometheus-stack-alertmanager
            app: alertmanager
        topologyKey: kubernetes.io/hostname
    weight: 100
# 容忍度。此处为空
# tolerations:
# - effect: NoSchedule
#   key: node-role.kubernetes.io/master
#   operator: Exists
# 资源需求。若存在多个容器，需填写 resources 总和
resources:
  limits:
    cpu: 100m
    memory: 256Mi
  requests:
    cpu: 100m
    memory: 32Mi
# 存储类模板
volumeClaimTemplates:
- apiVersion: v1
  kind: PersistentVolumeClaim
  metadata:
    name: alertmanager-kube-prometheus-stack-alertmanager-db
  spec:
    accessModes:
    - ReadWriteOnce
    resources:
      requests:
        storage: 1Gi
    volumeMode: Filesystem
```