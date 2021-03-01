// +build e2e

package e2e

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/clientcmd"

	ctrl "sigs.k8s.io/controller-runtime"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	hyperapi "github.com/openshift/hypershift/api"
	apifixtures "github.com/openshift/hypershift/api/fixtures"
	hyperv1 "github.com/openshift/hypershift/api/v1alpha1"
	"github.com/openshift/hypershift/version"
)

// QuickStartOptions are the raw user input used to construct the test input.
type QuickStartOptions struct {
	AWSCredentialsFile string
	PullSecretFile     string
	SSHKeyFile         string
	ReleaseImage       string
}

var quickStartOptions QuickStartOptions

func init() {
	flag.StringVar(&quickStartOptions.AWSCredentialsFile, "e2e.quick-start.aws-credentials-file", "", "path to AWS credentials")
	flag.StringVar(&quickStartOptions.PullSecretFile, "e2e.quick-start.pull-secret-file", "", "path to pull secret")
	flag.StringVar(&quickStartOptions.SSHKeyFile, "e2e.quick-start.ssh-key-file", filepath.Join(os.Getenv("HOME"), ".ssh", "id_rsa.pub"), "path to SSH public key")
	flag.StringVar(&quickStartOptions.ReleaseImage, "e2e.quick-start.release-image", "", "OCP release image to test")
}

// QuickStartInput are the validated options for running the test.
type QuickStartInput struct {
	Client         crclient.Client
	ReleaseImage   string
	AWSCredentials []byte
	PullSecret     []byte
	SSHKey         []byte
}

// GetContext builds a QuickStartInput from the options.
func (o QuickStartOptions) GetContext() (*QuickStartInput, error) {
	input := &QuickStartInput{}

	var err error
	input.PullSecret, err = ioutil.ReadFile(o.PullSecretFile)
	if err != nil {
		return nil, fmt.Errorf("couldn't read pull secret file %q: %w", o.PullSecretFile, err)
	}
	if len(input.PullSecret) == 0 {
		return nil, fmt.Errorf("pull secret is required")
	}

	input.AWSCredentials, err = ioutil.ReadFile(o.AWSCredentialsFile)
	if err != nil {
		return nil, fmt.Errorf("couldn't read aws credentials file %q: %w", o.AWSCredentialsFile, err)
	}
	if len(input.AWSCredentials) == 0 {
		return nil, fmt.Errorf("AWS credentials are required")
	}

	input.SSHKey, err = ioutil.ReadFile(o.SSHKeyFile)
	if err != nil {
		return nil, fmt.Errorf("couldn't read SSH key file %q: %w", o.SSHKeyFile, err)
	}
	if len(input.SSHKey) == 0 {
		return nil, fmt.Errorf("SSH key is required")
	}

	if len(o.ReleaseImage) == 0 {
		defaultVersion, err := version.LookupDefaultOCPVersion()
		if err != nil {
			return nil, fmt.Errorf("couldn't look up default OCP version: %w", err)
		}
		input.ReleaseImage = defaultVersion.PullSpec
	}
	if len(input.ReleaseImage) == 0 {
		return nil, fmt.Errorf("release image is required")
	}

	input.Client, err = crclient.New(ctrl.GetConfigOrDie(), crclient.Options{Scheme: hyperapi.Scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create kube client: %w", err)
	}

	return input, nil
}

// TestQuickStart implements a test that mimics the operation described in the
// HyperShift quick start (creating a basic guest cluster).
//
// This test is meant to provide a first, fast signal to detect regression; it
// is recommended to use it as a PR blocker test.
func TestQuickStart(t *testing.T) {
	input, err := quickStartOptions.GetContext()
	if err != nil {
		t.Fatalf("failed to create test context: %s", err)
	}

	t.Logf("Testing OCP release image %s", input.ReleaseImage)
	ctx := context.TODO()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "e2e-",
		},
	}
	err = input.Client.Create(ctx, namespace)
	if err != nil {
		t.Fatalf("failed to create namespace: %s", err)
	}
	if len(namespace.Name) == 0 {
		t.Fatalf("generated namespace has no name")
	}
	t.Logf("Created test namespace %s", namespace.Name)

	// Clean up the namespace after the test
	defer func() {
		err := input.Client.Delete(ctx, namespace, &crclient.DeleteOptions{})
		if err != nil {
			t.Fatalf("failed to delete namespace %q: %s", namespace.Name, err)
		}
		t.Logf("Waiting for the test namespace %q to be deleted", namespace.Name)
		err = wait.Poll(1*time.Second, 10*time.Minute, func() (done bool, err error) {
			latestNamespace := &corev1.Namespace{}
			key := crclient.ObjectKey{
				Name: namespace.Name,
			}
			if err := input.Client.Get(ctx, key, latestNamespace); err != nil {
				if errors.IsNotFound(err) {
					return true, nil
				}
				t.Logf("failed to get namespace %q: %s", latestNamespace.Name, err)
				return false, nil
			}
			return false, nil
		})
		if err != nil {
			t.Fatalf("failed to clean up namespace %q: %s", namespace.Name, err)
		}
	}()

	example := apifixtures.ExampleOptions{
		Namespace:        namespace.Name,
		Name:             "example-" + namespace.Name,
		ReleaseImage:     input.ReleaseImage,
		PullSecret:       input.PullSecret,
		AWSCredentials:   input.AWSCredentials,
		SSHKey:           input.SSHKey,
		NodePoolReplicas: 2,
	}.Resources()

	err = input.Client.Create(ctx, example.PullSecret)
	if err != nil {
		t.Fatalf("couldn't create pull secret: %s", err)
	}
	t.Logf("Created test pull secret %s", example.PullSecret.Name)

	err = input.Client.Create(ctx, example.AWSCredentials)
	if err != nil {
		t.Fatalf("couldn't create aws credentials secret: %s", err)
	}
	t.Logf("Created test aws credentials secret %s", example.AWSCredentials.Name)

	err = input.Client.Create(ctx, example.SSHKey)
	if err != nil {
		t.Fatalf("couldn't create ssh key secret: %s", err)
	}
	t.Logf("Created test ssh key secret %s", example.SSHKey.Name)

	err = input.Client.Create(ctx, example.Cluster)
	if err != nil {
		t.Fatalf("couldn't create cluster: %s", err)
	}
	t.Logf("Created test hostedcluster %s", example.Cluster.Name)

	// Perform some very basic assertions about the guest cluster
	t.Logf("Ensuring the guest cluster exposes a valid kubeconfig")

	t.Logf("Waiting for guest kubeconfig to become available")
	var guestKubeConfigSecret corev1.Secret
	err = wait.Poll(1*time.Second, 5*time.Minute, func() (done bool, err error) {
		var currentCluster hyperv1.HostedCluster
		err = input.Client.Get(ctx, crclient.ObjectKeyFromObject(example.Cluster), &currentCluster)
		if err != nil {
			t.Logf("error getting cluster: %s", err)
			return false, nil
		}
		if currentCluster.Status.KubeConfig == nil {
			return false, nil
		}
		key := crclient.ObjectKey{
			Namespace: currentCluster.Namespace,
			Name:      currentCluster.Status.KubeConfig.Name,
		}
		if err := input.Client.Get(ctx, key, &guestKubeConfigSecret); err != nil {
			t.Logf("failed to get guest kubeconfig secret %s: %s", key, err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("guest kubeconfig didn't become available")
	}

	// TODO: this key should probably be published or an API constant
	guestKubeConfigSecretData, hasData := guestKubeConfigSecret.Data["kubeconfig"]
	if !hasData {
		t.Fatalf("guest kubeconfig secret is missing kubeconfig key")
	}

	guestConfig, err := clientcmd.RESTConfigFromKubeConfig(guestKubeConfigSecretData)
	if err != nil {
		t.Fatalf("couldn't load guest kubeconfig: %s", err)
	}

	t.Logf("Establishing a connection to the guest apiserver")
	var guestClient crclient.Client
	err = wait.Poll(5*time.Second, 5*time.Minute, func() (done bool, err error) {
		kubeClient, err := crclient.New(guestConfig, crclient.Options{Scheme: hyperapi.Scheme})
		if err != nil {
			t.Logf("failed to create kube client: %s", err)
			return false, nil
		}
		guestClient = kubeClient
		return true, nil
	})
	if err != nil {
		t.Fatalf("failed to establish a connection to the guest apiserver: %s", err)
	}

	t.Logf("Ensuring guest nodes become ready")
	nodes := &corev1.NodeList{}
	err = wait.Poll(5*time.Second, 10*time.Minute, func() (done bool, err error) {
		err = guestClient.List(ctx, nodes)
		if err != nil {
			t.Logf("failed to list nodes: %s", err)
			return false, nil
		}
		if len(nodes.Items) == 0 {
			return false, nil
		}
		var readyNodes []string
		for _, node := range nodes.Items {
			for _, cond := range node.Status.Conditions {
				if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
					readyNodes = append(readyNodes, node.Name)
				}
			}
		}
		if len(readyNodes) != example.Cluster.Spec.InitialComputeReplicas {
			return false, nil
		}
		t.Logf("found %d ready nodes", len(nodes.Items))
		return true, nil
	})
	if err != nil {
		t.Fatalf("failed to ensure guest nodes became ready: %s", err)
	}
}
