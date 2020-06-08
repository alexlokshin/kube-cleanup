package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/urfave/cli/v2"

	"github.com/cheggaaa/pb"
	isd "github.com/jbenet/go-is-domain"
	"gopkg.in/yaml.v2"
	v1apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type ResourceReference struct {
	Name          string `json:",omitempty" yaml:",omitempty"`
	Kind          string `json:",omitempty" yaml:",omitempty"`
	LabelSelector string `json:",omitempty" yaml:",omitempty"`
}

type InventoryViolation struct {
	Name      string            `json:",omitempty" yaml:",omitempty"`
	Kind      string            `json:",omitempty" yaml:",omitempty"`
	Reference ResourceReference `json:",omitempty" yaml:",omitempty"`
	Reason    string            `json:",omitempty" yaml:",omitempty"`
}

type ResourceInventoryList struct {
	Items map[string]InventoryViolation `json:",omitempty" yaml:",omitempty"`
}

type Namespace struct {
	Namespace string               `json:",omitempty" yaml:",omitempty"`
	Items     []InventoryViolation `json:",omitempty" yaml:",omitempty"`
}

type NamespaceList struct {
	Namespaces []Namespace `json:",omitempty" yaml:",omitempty"`
}

var inCluster bool

func betterPanic(message string, args ...string) {
	temp := fmt.Sprintf(message, args)
	fmt.Printf("%s\n\n", temp)
	os.Exit(1)
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

func contains(s string, array []string) bool {
	for _, v := range array {
		if v == s {
			return true
		}
	}
	return false
}

func printReport(orphans map[string]ResourceInventoryList, outputMode string) {
	namespaceList := NamespaceList{}
	for namespace, ResourceInventoryList := range orphans {
		orphanedItems := make([]InventoryViolation, 0)

		for _, reason := range ResourceInventoryList.Items {
			orphanedItems = append(orphanedItems, reason)
		}

		ns := Namespace{Namespace: namespace, Items: orphanedItems}
		namespaceList.Namespaces = append(namespaceList.Namespaces, ns)
	}

	if len(orphans) == 0 {
		fmt.Printf("You don't have any problems, at all!\n")
	} else {
		if "text" == outputMode {
			for namespace, ResourceInventoryList := range orphans {
				fmt.Printf("\n==============================\n")
				fmt.Printf("Namespace: %s\n", namespace)
				fmt.Printf("==============================\n")

				if len(ResourceInventoryList.Items) > 0 {
					fmt.Printf("\nOrphaned Items\n")
					for ingress, reason := range ResourceInventoryList.Items {
						fmt.Printf("* %s, %s\n", ingress, reason)
					}
				}
				fmt.Println()
			}
		} else if "yaml" == outputMode {
			pretty, err := yaml.Marshal(&namespaceList)
			if err != nil {
				betterPanic(err.Error())
			}
			fmt.Println(string(pretty))
		} else if "json" == outputMode {
			pretty, err := json.MarshalIndent(namespaceList, "", "    ")
			if err != nil {
				betterPanic(err.Error())
			}
			fmt.Println(string(pretty))
		}
	}
}

func main() {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	var kubeconfig string
	var outputMode string
	namespace := ""
	home := homeDir()
	kubeConfigPath := ""
	if home != "" {
		kubeConfigPath = filepath.Join(home, ".kube", "config")
	}

	flags := []cli.Flag{
		&cli.StringFlag{
			Name:        "kubeconfig",
			Value:       kubeConfigPath,
			Usage:       "absolute path to the kubeconfig file",
			Destination: &kubeconfig,
		},
		&cli.StringFlag{
			Name:        "o",
			Aliases:     []string{"output"},
			Value:       "yaml",
			Usage:       "output format (yaml, json, kubectl)",
			Destination: &outputMode,
		},
		&cli.StringFlag{
			Name:        "n",
			Aliases:     []string{"namespace", "namespaces"},
			Value:       "",
			Usage:       "limit to this namespace (all namespaces if blank)",
			Destination: &namespace,
		},
	}

	app := &cli.App{
		Name:  "kube-cleanup",
		Usage: "kubernetes garbage collector",
		Flags: flags,
		Commands: []*cli.Command{
			{
				Name:    "validate",
				Aliases: []string{"val", "check"},
				Usage:   "validate resources",
				Subcommands: []*cli.Command{
					{
						Name:    "ns",
						Aliases: []string{"namespace", "namespaces"},
						Usage:   "validate namespace(s)",
						Flags:   flags,
						Action: func(c *cli.Context) error {
							orphans := validateNamespaces(kubeconfig)
							printReport(orphans, outputMode)
							return nil
						},
					},
					{
						Name:    "ing",
						Aliases: []string{"ingress", "ingresses"},
						Usage:   "validate ingress(s)",
						Flags:   flags,
						Action: func(c *cli.Context) error {
							orphans := validateIngresses(kubeconfig, namespace)
							printReport(orphans, outputMode)
							return nil
						},
					},
					{
						Name:    "svc",
						Aliases: []string{"service", "services"},
						Usage:   "validate service(s)",
						Flags:   flags,
						Action: func(c *cli.Context) error {
							orphans := validateServices(kubeconfig, namespace)
							printReport(orphans, outputMode)
							return nil
						},
					},
					{
						Name:    "dep",
						Aliases: []string{"deployment", "deployments"},
						Usage:   "validate deployment(s)",
						Flags:   flags,
						Action: func(c *cli.Context) error {
							orphans := validateDeployments(kubeconfig, namespace)
							printReport(orphans, outputMode)
							return nil
						},
					},
				},

				Action: func(c *cli.Context) error {
					//run(kubeconfig, outputMode)
					fmt.Printf("Running validation...")
					return nil
				},
			},
		},
		Action: func(c *cli.Context) error {
			fmt.Println("For usage, run ./kube-cleanup -?")
			cli.ShowAppHelp(c)

			return errors.New("Command not specified")
		},
	}

	sort.Sort(cli.FlagsByName(app.Flags))
	sort.Sort(cli.CommandsByName(app.Commands))

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}

}

func getKubernetesClient(kubeconfig string) (*kubernetes.Clientset, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Println("Local configuration not found, trying in-cluster configuration.")
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
		inCluster = true
	}
	inCluster = false

	if inCluster {
		log.Printf("Configured to run in in-cluster mode.\n")
	} else {
		log.Printf("Configured to run in out-of cluster mode.\n")
	}

	clientset, err := kubernetes.NewForConfig(config)
	return clientset, err
}

func addInventoryViolation(orphans map[string]ResourceInventoryList, namespace string, name string, reason InventoryViolation) {
	inventoryList, ok := orphans[namespace]
	if !ok {
		inventoryList = ResourceInventoryList{Items: make(map[string]InventoryViolation)}
	}
	inventoryList.Items[name] = reason
	orphans[namespace] = inventoryList
}

func validateIngresses(kubeconfig string, namespace string) map[string]ResourceInventoryList {
	clientset, err := getKubernetesClient(kubeconfig)
	if err != nil {
		betterPanic("Unable to connect to K8s: %s", err.Error())
	}

	ingresses, err := clientset.ExtensionsV1beta1().Ingresses(namespace).List(metav1.ListOptions{})
	if err != nil {
		betterPanic("Unable to retrieve ingresses: %s", err.Error())
	}

	orphans := make(map[string]ResourceInventoryList)

	fmt.Printf("Examining ingress rules.\n")
	bar := pb.StartNew(len(ingresses.Items))
	for _, ingress := range ingresses.Items {
		bar.Increment()

		for _, rule := range ingress.Spec.Rules {
			if rule.HTTP == nil {
				addInventoryViolation(orphans, ingress.Namespace, ingress.Name, InventoryViolation{Reason: "no HTTP routes in ingress", Kind: "ingress", Name: ingress.Name})
				continue
			}
			for _, path := range rule.HTTP.Paths {

				serviceName := path.Backend.ServiceName
				servicePort := path.Backend.ServicePort.IntVal
				service, err := clientset.CoreV1().Services(ingress.Namespace).Get(serviceName, metav1.GetOptions{})
				if err != nil {
					addInventoryViolation(orphans, ingress.Namespace, ingress.Name, InventoryViolation{Reason: "references a missing service: " + err.Error(), Kind: "ingress", Reference: ResourceReference{Kind: "service", Name: serviceName}, Name: ingress.Name})
					continue
				}

				found := false
				for _, port := range service.Spec.Ports {
					if port.Port == servicePort {
						found = true
						break
					}
				}

				if !found {
					addInventoryViolation(orphans, ingress.Namespace, ingress.Name, InventoryViolation{Reason: fmt.Sprintf("Service doesn't expose ingress port %d", servicePort), Kind: "ingress", Reference: ResourceReference{Kind: "service", Name: serviceName}, Name: ingress.Name})
					continue
				}
			}
		}
	}
	bar.Finish()
	return orphans
}

func validateNamespaces(kubeconfig string) map[string]ResourceInventoryList {
	orphans := make(map[string]ResourceInventoryList)

	clientset, err := getKubernetesClient(kubeconfig)
	if err != nil {
		betterPanic("Unable to connect to K8s: %s", err.Error())
	}

	namespaces, err := clientset.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		betterPanic("Unable to retrieve namespaces: %s", err.Error())
	}

	bar := pb.StartNew(len(namespaces.Items))
	for _, namespace := range namespaces.Items {
		bar.Increment()
		if namespace.Status.Phase == v1.NamespaceTerminating && contains("kubernetes", namespace.Finalizers) {
			addInventoryViolation(orphans, namespace.Namespace, namespace.Name, InventoryViolation{Reason: "stuck in termination", Kind: "ingress", Name: namespace.Namespace})
		}
	}
	bar.Finish()

	return orphans
}

func validateServices(kubeconfig string, namespace string) map[string]ResourceInventoryList {
	orphans := make(map[string]ResourceInventoryList)
	clientset, err := getKubernetesClient(kubeconfig)
	if err != nil {
		betterPanic("Unable to connect to K8s: %s", err.Error())
	}

	services, err := clientset.CoreV1().Services(namespace).List(metav1.ListOptions{})
	if err != nil {
		betterPanic("Unable to retrieve services: %s", err.Error())
	}

	bar := pb.StartNew(len(services.Items))
	for _, service := range services.Items {
		bar.Increment()
		if "default" == service.Namespace && "kubernetes" == service.Name {
			continue
		}
		// No selector on the service, i.e. calls cannot be routed
		if len(service.Spec.Selector) == 0 && service.Spec.Type != v1.ServiceTypeExternalName {
			addInventoryViolation(orphans, service.Namespace, service.Name, InventoryViolation{Reason: "no selector", Kind: "service", Name: service.Name})

			continue
		}

		if service.Spec.Type == v1.ServiceTypeLoadBalancer {
			if len(service.Status.LoadBalancer.Ingress) == 0 {
				addInventoryViolation(orphans, service.Namespace, service.Name, InventoryViolation{Reason: "LoadBalancer service in pending state", Kind: "service", Name: service.Name})

			}
			continue
		}

		if service.Spec.Type == v1.ServiceTypeExternalName {
			if !isd.IsDomain(service.Spec.ExternalName) {
				addInventoryViolation(orphans, service.Namespace, service.Name, InventoryViolation{Reason: fmt.Sprintf("%s is not a valid CNAME", service.Spec.ExternalName), Kind: "service", Name: service.Name})
			}
			continue
		}

		listOptions := metav1.ListOptions{}
		listOptions.LabelSelector = labels.SelectorFromSet(service.Spec.Selector).String()

		podList, err := clientset.CoreV1().Pods(namespace).List(listOptions)

		if err != nil {
			addInventoryViolation(orphans, service.Namespace, service.Name, InventoryViolation{Reason: "backing service references no workloads: " + err.Error(), Kind: "service", Name: service.Name})
			continue
		}

		if len(podList.Items) == 0 {
			addInventoryViolation(orphans, service.Namespace, service.Name, InventoryViolation{Reason: "backing workload contains no pods", Kind: "service", Reference: ResourceReference{Kind: "pod", LabelSelector: listOptions.LabelSelector}, Name: service.Name})

			continue
		}

	}
	bar.Finish()
	return orphans
}

func validateDeployments(kubeconfig string, namespace string) map[string]ResourceInventoryList {
	orphans := make(map[string]ResourceInventoryList)
	clientset, err := getKubernetesClient(kubeconfig)
	if err != nil {
		betterPanic("Unable to connect to K8s: %s", err.Error())
	}

	deployments, err := clientset.AppsV1().Deployments(namespace).List(metav1.ListOptions{})
	if err != nil {
		betterPanic("Unable to retrieve deployments: %s", err.Error())
	}

	bar := pb.StartNew(len(deployments.Items))
	for _, deployment := range deployments.Items {
		bar.Increment()

		// No selector on the service, i.e. calls cannot be routed
		if deployment.Status.Replicas == 0 {
			addInventoryViolation(orphans, deployment.Namespace, deployment.Name, InventoryViolation{Reason: "deployment scaled down to 0 replicas", Kind: "deployment", Name: deployment.Name})
			continue
		}

		if len(deployment.Labels) == 0 {
			addInventoryViolation(orphans, deployment.Namespace, deployment.Name, InventoryViolation{Reason: "no labels on deployment", Kind: "deployment", Name: deployment.Name})
			continue
		}

		for _, condition := range deployment.Status.Conditions {
			if condition.Type == v1apps.DeploymentAvailable && condition.Status == "False" && condition.Reason == "MinimumReplicasUnavailable" {
				addInventoryViolation(orphans, deployment.Namespace, deployment.Name, InventoryViolation{Reason: "minimum replicas unavailable, could be temporary", Kind: "deployment", Name: deployment.Name})
				continue
			}

			if condition.Type == v1apps.DeploymentProgressing && condition.Status == "False" && condition.Reason == "ProgressDeadlineExceeded" {
				fmt.Printf("%v\n", condition)
				addInventoryViolation(orphans, deployment.Namespace, deployment.Name, InventoryViolation{Reason: condition.Message, Kind: "deployment", Name: deployment.Name})
				continue
			}
		}
		if deployment.Status.ReadyReplicas == 0 {
			addInventoryViolation(orphans, deployment.Namespace, deployment.Name, InventoryViolation{Reason: "no replicas are ready", Kind: "deployment", Name: deployment.Name})
			continue
		}
	}
	bar.Finish()
	return orphans
}

// func run(kubeconfig string, outputMode string) {
// 	clientset, err := getKubernetesClient(kubeconfig)
// 	if err != nil {
// 		betterPanic(err.Error())
// 	}

// 	ingresses, err := clientset.ExtensionsV1beta1().Ingresses("").List(metav1.ListOptions{})
// 	if err != nil {
// 		betterPanic(err.Error())
// 	}

// 	orphans := make(map[string]ResourceInventoryList)

// 	fmt.Printf("Examining ingress rules.\n")
// 	bar := pb.StartNew(len(ingresses.Items))
// 	for _, ingress := range ingresses.Items {
// 		bar.Increment()

// 		for _, rule := range ingress.Spec.Rules {
// 			if rule.HTTP == nil {
// 				ResourceInventoryList := orphans[ingress.Namespace]
// 				if ResourceInventoryList.Ingresses == nil {
// 					ResourceInventoryList.Ingresses = make(map[string]InventoryViolation)
// 				}
// 				ResourceInventoryList.Ingresses[ingress.Name] = InventoryViolation{Reason: "no HTTP routes in ingress", Kind: "ingress", Name: ingress.Name}
// 				orphans[ingress.Namespace] = ResourceInventoryList
// 				continue
// 			}
// 			for _, path := range rule.HTTP.Paths {

// 				serviceName := path.Backend.ServiceName
// 				servicePort := path.Backend.ServicePort.IntVal
// 				service, err := clientset.CoreV1().Services(ingress.Namespace).Get(serviceName, metav1.GetOptions{})
// 				if err != nil {
// 					ResourceInventoryList := orphans[ingress.Namespace]
// 					if ResourceInventoryList.Ingresses == nil {
// 						ResourceInventoryList.Ingresses = make(map[string]InventoryViolation)
// 					}
// 					ResourceInventoryList.Ingresses[ingress.Name] = InventoryViolation{Reason: "references a missing service: " + err.Error(), Kind: "ingress", Reference: ResourceReference{Kind: "service", Name: serviceName}, Name: ingress.Name}
// 					orphans[ingress.Namespace] = ResourceInventoryList

// 					continue
// 				}

// 				found := false
// 				for _, port := range service.Spec.Ports {
// 					if port.Port == servicePort {
// 						found = true
// 						break
// 					}
// 				}

// 				if !found {
// 					ResourceInventoryList := orphans[ingress.Namespace]
// 					if ResourceInventoryList.Ingresses == nil {
// 						ResourceInventoryList.Ingresses = make(map[string]InventoryViolation)
// 					}
// 					ResourceInventoryList.Ingresses[ingress.Name] = InventoryViolation{Reason: fmt.Sprintf("Service doesn't expose ingress port %d", servicePort), Kind: "ingress", Reference: ResourceReference{Kind: "service", Name: serviceName}, Name: ingress.Name}
// 					orphans[ingress.Namespace] = ResourceInventoryList

// 					continue
// 				}

// 				if len(service.Spec.Selector) > 0 {

// 					listOptions := metav1.ListOptions{}
// 					listOptions.LabelSelector = labels.SelectorFromSet(service.Spec.Selector).String()

// 					podList, err := clientset.CoreV1().Pods(ingress.Namespace).List(listOptions)

// 					if err != nil {
// 						ResourceInventoryList := orphans[ingress.Namespace]
// 						if ResourceInventoryList.Ingresses == nil {
// 							ResourceInventoryList.Ingresses = make(map[string]InventoryViolation)
// 						}
// 						ResourceInventoryList.Ingresses[ingress.Name] = InventoryViolation{Reason: "backing service references no workloads: " + err.Error(), Kind: "ingress", Reference: ResourceReference{Kind: "service", Name: serviceName}, Name: ingress.Name}
// 						orphans[ingress.Namespace] = ResourceInventoryList

// 						continue
// 					}

// 					if len(podList.Items) == 0 {
// 						ResourceInventoryList := orphans[ingress.Namespace]
// 						if ResourceInventoryList.Ingresses == nil {
// 							ResourceInventoryList.Ingresses = make(map[string]InventoryViolation)
// 						}
// 						ResourceInventoryList.Ingresses[ingress.Name] = InventoryViolation{Reason: "backing workload contains no pods", Kind: "ingress", Reference: ResourceReference{Kind: "pods", Name: listOptions.LabelSelector}, Name: ingress.Name}
// 						orphans[ingress.Namespace] = ResourceInventoryList

// 						continue
// 					}
// 				}

// 			}
// 		}
// 	}
// 	bar.Finish()

// 	pods, err := clientset.CoreV1().Pods("").List(metav1.ListOptions{})
// 	if err != nil {
// 		betterPanic(err.Error())
// 	}

// 	bar = pb.StartNew(len(pods.Items))

// 	fmt.Printf("Examining orphaned pods.\n")
// 	for _, pod := range pods.Items {
// 		bar.Increment()
// 		if "kube-system" == pod.Namespace {
// 			continue
// 		}

// 		ownerReferences := pod.GetObjectMeta().GetOwnerReferences()
// 		for _, ownerReference := range ownerReferences {
// 			if "ReplicaSet" == ownerReference.Kind {
// 				rs, err := clientset.ExtensionsV1beta1().ReplicaSets(pod.Namespace).Get(ownerReference.Name, metav1.GetOptions{})
// 				if err != nil {
// 					ResourceInventoryList := orphans[pod.Namespace]
// 					if ResourceInventoryList.Pods == nil {
// 						ResourceInventoryList.Pods = make(map[string]InventoryViolation)
// 					}
// 					ResourceInventoryList.Pods[pod.Name] = InventoryViolation{Reason: "owner is missing", Name: pod.Name, Kind: "pod", Reference: ResourceReference{Kind: ownerReference.Kind, Name: ownerReference.Name}}
// 					orphans[pod.Namespace] = ResourceInventoryList

// 					continue
// 				} else {
// 					for _, ownerReference := range rs.OwnerReferences {
// 						if "Deployment" == ownerReference.Kind {
// 							_, err := clientset.ExtensionsV1beta1().Deployments(rs.Namespace).Get(ownerReference.Name, metav1.GetOptions{})
// 							if err != nil {
// 								ResourceInventoryList := orphans[pod.Namespace]
// 								if ResourceInventoryList.Pods == nil {
// 									ResourceInventoryList.Pods = make(map[string]InventoryViolation)
// 								}
// 								ResourceInventoryList.Pods[pod.Name] = InventoryViolation{Reason: "owner of the owner is missing", Kind: "pod", Name: pod.Name, Reference: ResourceReference{Kind: "deployment", Name: ownerReference.Name}}
// 								orphans[pod.Namespace] = ResourceInventoryList
// 							}
// 						}
// 					}
// 				}
// 			}
// 		}
// 		if len(ownerReferences) == 0 {
// 			ResourceInventoryList := orphans[pod.Namespace]
// 			if ResourceInventoryList.Pods == nil {
// 				ResourceInventoryList.Pods = make(map[string]InventoryViolation)
// 			}
// 			ResourceInventoryList.Pods[pod.Name] = InventoryViolation{Reason: "pod is not owned by anyone", Name: pod.Name, Kind: "pod"}
// 			orphans[pod.Namespace] = ResourceInventoryList
// 		}
// 	}
// 	bar.Finish()

// 	namespaceList := NamespaceList{}
// 	for namespace, ResourceInventoryList := range orphans {
// 		orphanedIngresses := make([]InventoryViolation, 0)
// 		orphanedPods := make([]InventoryViolation, 0)

// 		for _, reason := range ResourceInventoryList.Ingresses {
// 			orphanedIngresses = append(orphanedIngresses, reason)
// 		}

// 		for _, reason := range ResourceInventoryList.Pods {
// 			orphanedPods = append(orphanedPods, reason)
// 		}

// 		ns := Namespace{Name: namespace, Ingresses: orphanedIngresses, Pods: orphanedPods}
// 		namespaceList.Namespaces = append(namespaceList.Namespaces, ns)
// 	}

// 	if len(orphans) == 0 {
// 		fmt.Printf("You don't have any problems, at all!\n")
// 	} else {
// 		if "text" == outputMode {
// 			for namespace, ResourceInventoryList := range orphans {
// 				fmt.Printf("\n==============================\n")
// 				fmt.Printf("Namespace: %s\n", namespace)
// 				fmt.Printf("==============================\n")
// 				if len(ResourceInventoryList.Pods) > 0 {
// 					fmt.Printf("\nOrphaned Pods\n")
// 					for pod, reason := range ResourceInventoryList.Pods {
// 						fmt.Printf("* %s, %s\n", pod, reason)
// 					}
// 				}
// 				if len(ResourceInventoryList.Ingresses) > 0 {
// 					fmt.Printf("\nOrphaned Ingresses\n")
// 					for ingress, reason := range ResourceInventoryList.Ingresses {
// 						fmt.Printf("* %s, %s\n", ingress, reason)
// 					}
// 				}
// 				fmt.Println()
// 			}
// 		} else if "yaml" == outputMode {
// 			pretty, err := yaml.Marshal(&namespaceList)
// 			if err != nil {
// 				betterPanic(err.Error())
// 			}
// 			fmt.Println(string(pretty))
// 		} else if "json" == outputMode {
// 			pretty, err := json.MarshalIndent(namespaceList, "", "    ")
// 			if err != nil {
// 				betterPanic(err.Error())
// 			}
// 			fmt.Println(string(pretty))
// 		}
// 	}
// }
