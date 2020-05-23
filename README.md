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