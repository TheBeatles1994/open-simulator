# Motivation
## 概念定义
**Open-Simulator** 是 K8s 下的**仿真调度组件**。用户准备一批待创建 Workload 资源，Workload 资源指定好资源配额、绑核规则、亲和性规则、优先级等，通过 **Open-Simulator 的仿真调度能力**可判断当前集群是否能够满足 Workload 资源，以及添加多少资源可保证资源部署成功。

原生 Kubernetes 缺少**仿真调度能力**，且社区并没有相关项目供参考。**Open-Simulator** 可解决资源规划问题，通过Workload 调度要求计算出最少物理资源数量，进而提高资源使用率，为用户节省物理成本和运维成本。

# Use Case
两类场景需要资源规划：
- **交付前**：评估产品最少物理资源，通过仿真系统计算出交付需要的特定规格节点数量、磁盘数量（类似朱雀系统）；
- **运行时**：用户新建 or 扩容 Workload，仿真调度系统会给出当前集群物理资源是否满足，并给出集群扩容建议（详细到扩容节点数）

# Run

## 使用
### 添加节点

执行命令

./simon apply --kubeconfig=[kubeconfig文件目录] -f [Yaml文件夹目录]

Yaml文件夹参考./example目录，包含如下文件:
- Deployment yamls
- Statefulset yamls
- Node yaml

执行后输出一个名为configmap-simon.yaml的文件，用以保存结果。

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: simulator-plan
  namespace: kube-system
data:
  Deployment: '{"vivo-test-namespace/suppress-memcache-lsr":["simulator-node1","simulator-node1","node3","node2"],"vivo-test-namespace/suppress-memcache-be":["simulator-node1","simulator-node1","node3","node2"]}'
  StatefulSet: '{"vivo-test-namespace/suppress-memcache-lsr":["simulator-node1","simulator-node1","node3","node2"],"vivo-test-namespace/suppress-memcache-be":["simulator-node1","simulator-node1","node3","node2"]}'
```

## 效果图
![](doc/images/simon.png)
# Deployment

> 以 MacBook 为例

## 步骤
1. 克隆项目
   1. mkdir $(GOPATH)/github.com/alibaba
   2. cd $(GOPATH)/github.com/alibaba
   3. git clone https://github.com/alibaba/open-simulator.git
   4. cd open-simulator
2. 安装 [Minikube](https://minikube.sigs.k8s.io/docs/start/)
3. 运行 Minikube
   1. minikube start
4. 拷贝 kubeconfig 文件到项目目录
   1. cp ~/.kube/config  ./kubeconfig
5. 项目编译及运行
   1. make
   2. bin/simon apply --kubeconfig=./kubeconfig -f ./example/simple_example_by_huizhi