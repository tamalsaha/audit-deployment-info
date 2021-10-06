package main

import (
	"context"
	"fmt"
	"k8s.io/client-go/kubernetes"
	"log"
	"path/filepath"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/util/homedir"
	"kmodules.xyz/client-go/tools/exec"
)

func main() {
	masterURL := ""
	kubeconfigPath := filepath.Join(homedir.HomeDir(), ".kube", "config")

	config, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfigPath)
	if err != nil {
		log.Fatalf("Could not get Kubernetes config: %s", err)
	}

	kc := kubernetes.NewForConfigOrDie(config)

	factory := informers.NewSharedInformerFactory(kc, 0)
	nodeInformer := factory.Core().V1().Nodes()
	nodeLister := factory.Kubedb().V1alpha2().Postgreses().Lister()
	nodeInformer.AddEventHandler(nil) // c.Auditor.ForGVK(api.SchemeGroupVersion.WithKind(api.ResourceKindPostgres)))


	dc2 := dynamic.NewForConfigOrDie(config)

	gvrNode := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "nodes",
	}
	nodes, err := dc2.Resource(gvrNode).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err)
	}
	for _, obj := range nodes.Items {
		fmt.Printf("%+v\n", obj.GetName())
	}
	nodes, err = dc2.Resource(gvrNode).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err)
	}
	for _, obj := range nodes.Items {
		fmt.Printf("%+v\n", obj.GetName())
	}

	pod := types.NamespacedName{
		Namespace: "demo",
		Name:      "voyager-test-ingress-754d884cf-dmwrd",
	}
	out, err := exec.Exec(config, pod, exec.Container("haproxy"), exec.Command("ps"))
	if err != nil {
		panic(err)
	}
	fmt.Println(out)

	out22, err := exec.Exec(
		config,
		pod,
		exec.Container("haproxy"),
		exec.Command("cat", "/shared/haproxy.pid"))
	if err != nil {
		panic(err)
	}
	fmt.Println(out22)
	out2, err := exec.Exec(config, pod, exec.Container("haproxy"), exec.Command("/bin/kill", "-SIGHUP", strings.TrimSpace(out22)))
	if err != nil {
		panic(err)
	}
	fmt.Println(out2)

	time.Sleep(2 * time.Second)
	out3, err := exec.Exec(config, pod, exec.Container("haproxy"), exec.Command("ps"))
	if err != nil {
		panic(err)
	}
	fmt.Println(out3)
}
