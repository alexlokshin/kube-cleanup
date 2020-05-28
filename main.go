package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/cheggaaa/pb"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type OrphanReason struct {
	Name          string `json:",omitempty"`
	Kind          string `json:",omitempty"`
	Reference     string `json:",omitempty"`
	ReferenceKind string `json:",omitempty"`
	Reason        string `json:",omitempty"`
}
type OrphanList struct {
	Pods      map[string]OrphanReason `json:",omitempty"`
	Ingresses map[string]OrphanReason `json:",omitempty"`
}

type Namespace struct {
	Name      string          `json:",omitempty"`
	Pods      *[]OrphanReason `json:",omitempty"`
	Ingresses *[]OrphanReason `json:",omitempty"`
}

type NamespaceList struct {
	Namespaces []Namespace `json:",omitempty"`
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

func main() {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	var kubeconfig *string
	var outputMode *string
	home := homeDir()
	if home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	outputMode = flag.String("o", "text", "output mode (json,yaml,text)")
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Println("Local configuration not found, trying in-cluster configuration.")
		config, err = rest.InClusterConfig()
		if err != nil {
			betterPanic(err.Error())
		}
		inCluster = true
	}
	inCluster = false

	if inCluster {
		log.Printf("Configured to run in in-cluster mode.\n")
	} else {
		log.Printf("Configured to run in out-of cluster mode.\n")
	}

	log.Printf("Starting kube-cleanup.\n")

	clientset, err := kubernetes.NewForConfig(config)

	ingresses, err := clientset.ExtensionsV1beta1().Ingresses("").List(metav1.ListOptions{})
	if err != nil {
		betterPanic(err.Error())
	}

	orphans := make(map[string]OrphanList)

	fmt.Printf("Examining ingress rules.\n")
	bar := pb.StartNew(len(ingresses.Items))
	for _, ingress := range ingresses.Items {
		bar.Increment()

		for _, rule := range ingress.Spec.Rules {
			if rule.HTTP == nil {
				orphanList := orphans[ingress.Namespace]
				if orphanList.Ingresses == nil {
					orphanList.Ingresses = make(map[string]OrphanReason)
				}
				orphanList.Ingresses[ingress.Name] = OrphanReason{Reason: "no HTTP routes in ingress", Kind: "ingress", Name: ingress.Name}
				orphans[ingress.Namespace] = orphanList
				continue
			}
			for _, path := range rule.HTTP.Paths {

				serviceName := path.Backend.ServiceName
				_, err := clientset.CoreV1().Services(ingress.Namespace).Get(serviceName, metav1.GetOptions{})
				if err != nil {
					orphanList := orphans[ingress.Namespace]
					if orphanList.Ingresses == nil {
						orphanList.Ingresses = make(map[string]OrphanReason)
					}
					orphanList.Ingresses[ingress.Name] = OrphanReason{Reason: "references a missing service", Kind: "ingress", ReferenceKind: "service", Reference: serviceName, Name: ingress.Name}
					orphans[ingress.Namespace] = orphanList

					continue
				}
			}
		}
	}
	bar.Finish()

	pods, err := clientset.CoreV1().Pods("").List(metav1.ListOptions{})
	if err != nil {
		betterPanic(err.Error())
	}

	bar = pb.StartNew(len(pods.Items))

	fmt.Printf("Examining orphaned pods.\n")
	for _, pod := range pods.Items {
		bar.Increment()
		if "kube-system" == pod.Namespace {
			continue
		}

		ownerReferences := pod.GetObjectMeta().GetOwnerReferences()
		for _, ownerReference := range ownerReferences {
			if "ReplicaSet" == ownerReference.Kind {
				rs, err := clientset.ExtensionsV1beta1().ReplicaSets(pod.Namespace).Get(ownerReference.Name, metav1.GetOptions{})
				if err != nil {
					orphanList := orphans[pod.Namespace]
					if orphanList.Pods == nil {
						orphanList.Pods = make(map[string]OrphanReason)
					}
					orphanList.Pods[pod.Name] = OrphanReason{Reason: "owner is missing", Name: pod.Name, Kind: "pod", ReferenceKind: ownerReference.Kind, Reference: ownerReference.Name}
					orphans[pod.Namespace] = orphanList

					continue
				} else {
					for _, ownerReference := range rs.OwnerReferences {
						if "Deployment" == ownerReference.Kind {
							_, err := clientset.ExtensionsV1beta1().Deployments(rs.Namespace).Get(ownerReference.Name, metav1.GetOptions{})
							if err != nil {
								orphanList := orphans[pod.Namespace]
								if orphanList.Pods == nil {
									orphanList.Pods = make(map[string]OrphanReason)
								}
								orphanList.Pods[pod.Name] = OrphanReason{Reason: "owner of the owner is missing", Kind: "pod", Name: pod.Name, ReferenceKind: "deployment", Reference: ownerReference.Name}
								orphans[pod.Namespace] = orphanList
							}
						}
					}
				}
			}
		}
		if len(ownerReferences) == 0 {
			orphanList := orphans[pod.Namespace]
			if orphanList.Pods == nil {
				orphanList.Pods = make(map[string]OrphanReason)
			}
			orphanList.Pods[pod.Name] = OrphanReason{Reason: "pod is not owned by anyone", Name: pod.Name, Kind: "pod"}
			orphans[pod.Namespace] = orphanList
		}
	}
	bar.Finish()

	namespaceList := NamespaceList{}
	for namespace, orphanList := range orphans {
		ns := Namespace{Name: namespace}
		namespaceList.Namespaces = append(namespaceList.Namespaces, ns)

		for _, reason := range orphanList.Ingresses {
			*ns.Ingresses = append(*ns.Ingresses, reason)
		}

		for _, reason := range orphanList.Pods {
			*ns.Pods = append(*ns.Pods, reason)
		}
	}

	if "text" == *outputMode {
		for namespace, orphanList := range orphans {
			fmt.Printf("\n==============================\n")
			fmt.Printf("Namespace: %s\n", namespace)
			fmt.Printf("==============================\n")
			if len(orphanList.Pods) > 0 {
				fmt.Printf("\nOrphaned Pods\n")
				for pod, reason := range orphanList.Pods {
					fmt.Printf("* %s, %s\n", pod, reason)
				}
			}
			if len(orphanList.Ingresses) > 0 {
				fmt.Printf("\nOrphaned Ingresses\n")
				for ingress, reason := range orphanList.Ingresses {
					fmt.Printf("* %s, %s\n", ingress, reason)
				}
			}
			fmt.Println()
		}
	} else if "yaml" == *outputMode {
		pretty, err := yaml.Marshal(&namespaceList)
		if err != nil {
			betterPanic(err.Error())
		}
		fmt.Println(string(pretty))
	} else if "json" == *outputMode {
		pretty, err := json.MarshalIndent(namespaceList, "", "    ")
		if err != nil {
			betterPanic(err.Error())
		}
		fmt.Println(string(pretty))
	}
}
