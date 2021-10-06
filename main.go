package main

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	v "gomodules.xyz/x/version"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"kmodules.xyz/client-go/tools/exec"
	"log"
	"math/big"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"
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
	nodeInformer := factory.Core().V1().Nodes().Informer()
	nodeLister := factory.Core().V1().Nodes().Lister()
	nodeInformer.AddEventHandlerWithResyncPeriod(nil, 0) // c.Auditor.ForGVK(api.SchemeGroupVersion.WithKind(api.ResourceKindPostgres)))

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

func GenerateSiteInfo() SiteInfo {
	var si SiteInfo
	si.Version = Version{
		Version:         v.Version.Version,
		VersionStrategy: v.Version.VersionStrategy,
		CommitHash:      v.Version.CommitHash,
		GitBranch:       v.Version.GitBranch,
		GitTag:          v.Version.GitTag,
		CommitTimestamp: v.Version.CommitTimestamp,
		GoVersion:       v.Version.GoVersion,
		Compiler:        v.Version.Compiler,
		Platform:        v.Version.Platform,
	}
	return si
}

type SiteInfo struct {
	Version Version `json:"version"`
}

type Version struct {
	Version         string `json:"version,omitempty"`
	VersionStrategy string `json:"versionStrategy,omitempty"`
	CommitHash      string `json:"commitHash,omitempty"`
	GitBranch       string `json:"gitBranch,omitempty"`
	GitTag          string `json:"gitTag,omitempty"`
	CommitTimestamp string `json:"commitTimestamp,omitempty"`
	GoVersion       string `json:"goVersion,omitempty"`
	Compiler        string `json:"compiler,omitempty"`
	Platform        string `json:"platform,omitempty"`
}

type KubernetesInfo struct {
	// https://github.com/kmodules/client-go/blob/master/tools/clusterid/lib.go
	ClusterName   string                    `json:"cluster_name,omitempty"`
	ClusterUID    string                    `json:"cluster_uid,omitempty"`
	Version       *version.Info             `json:"version,omitempty"`
	NodeCount     int                       `json:"node_count,omitempty"`
	NodeResources core.ResourceRequirements `json:"node_resources"`
	Certificate   *Certificate              `json:"certificate,omitempty"`
}

// https://github.com/kmodules/client-go/blob/kubernetes-1.16.3/tools/analytics/analytics.go#L66

type Certificate struct {
	Version             int       `json:"version,omitempty"`
	SerialNumber        *big.Int  `json:"serial_number,omitempty"`
	Issuer              pkix.Name `json:"issuer"`
	Subject             pkix.Name `json:"subject"`
	NotBefore, NotAfter time.Time // Validity bounds.

	// Subject Alternate Name values. (Note that these values may not be valid
	// if invalid values were contained within a parsed certificate. For
	// example, an element of DNSNames may not be a valid DNS domain name.)
	DNSNames       []string   `json:"dns_names,omitempty"`
	EmailAddresses []string   `json:"email_addresses,omitempty"`
	IPAddresses    []net.IP   `json:"ip_addresses,omitempty"`
	URIs           []*url.URL `json:"ur_is,omitempty"`
}
