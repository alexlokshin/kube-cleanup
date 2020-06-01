# Kube Cleanup

## Dependency discovery

Discovers dependency trees for deployed applications. Here are examples of dependencies:

* Ingress -> Service -> Deployment
* Ingress -> Service -> DaemonSet
* Ingress -> Service -> StatefulSet
* Ingress -> Service -> pods by selector
* Ingress -> Service -> pods by selector
* Ingress -> ExternalName service
* Service -> Deployment
* Service -> DaemonSet    
* Service -> StatefulSet
* Service -> pods by selector    

Tool will alert you if the route is not traversible and there's nothing on the other end. For Deployments, DaemonSets, StatefulSets we look for running pods. For services, we look for endpoints. We also look for deficiencies in ClusterIP, NodePort, Loadbalancer and ExternalName services (like pending states etc). Each successful route is then reported on. Unsuccessful routes are presented for cleanup. 

## Cleanup

## Integrity checks


## TODOs
* Add sample invalid resources
* Transition to a configuration model
* Reduce validation loops. For ingresses only make sure services exist. Loop through services separately, making sure their workloads exist.
* For Deployments/DaemonSets etc make sure pods not only exist, but are running. If not a single pod was running for a while, report on the workload.
* Validate resource versions
* Help with the annotation validation
* kubernetes.io/ingress.class annotation migrated to spec.ingressClassName and ingressClass resources
* For LoadBalancer services, report if the actual LB creation took too long.
* Complain about services without a selector (unless that's an externalname service)
* For externalname service complain, if it points to an IP address and not a CNAME
* Fix namespaces stuck in terminating state