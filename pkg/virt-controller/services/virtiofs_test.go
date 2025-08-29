package services

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"kubevirt.io/kubevirt/pkg/pointer"
	virtconfig "kubevirt.io/kubevirt/pkg/virt-config"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/api"

	"kubevirt.io/kubevirt/pkg/testutils"
	"kubevirt.io/kubevirt/pkg/virt-config/featuregate"
)

var _ = Describe("virtiofs container", func() {

	kv := &v1.KubeVirt{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubevirt",
			Namespace: "kubevirt",
		},
		Spec: v1.KubeVirtSpec{
			Configuration: v1.KubeVirtConfiguration{
				DeveloperConfiguration: &v1.DeveloperConfiguration{},
			},
		},
		Status: v1.KubeVirtStatus{
			Phase:               v1.KubeVirtPhaseDeploying,
			DefaultArchitecture: "amd64",
		},
	}
	config, _, kvStore := testutils.NewFakeClusterConfigUsingKV(kv)

	enableFeatureGate := func(featureGate string) {
		testutils.UpdateFakeKubeVirtClusterConfig(kvStore, &v1.KubeVirt{
			Spec: v1.KubeVirtSpec{
				Configuration: v1.KubeVirtConfiguration{
					DeveloperConfiguration: &v1.DeveloperConfiguration{
						FeatureGates: []string{featureGate},
					},
				},
			},
		})
	}

	disableFeatureGates := func() {
		testutils.UpdateFakeKubeVirtClusterConfig(kvStore, kv)
	}

	BeforeEach(func() {
		enableFeatureGate(featuregate.VirtIOFSStorageVolumeGate)
		enableFeatureGate(featuregate.VirtIOFSConfigVolumesGate)
	})

	AfterEach(func() {
		disableFeatureGates()
	})

	type testEntry struct {
		cachePolicy *v1.VirtioFSCachingPolicy
	}

	DescribeTable("should create unprivileged containers only", func(entry testEntry) {
		By(fmt.Sprintf("applying KubeVirt config with virtiofsd cache policy=%v", entry.cachePolicy))
		if entry.cachePolicy != nil {
			testutils.UpdateFakeKubeVirtClusterConfig(kvStore, &v1.KubeVirt{
				Spec: v1.KubeVirtSpec{
					Configuration: v1.KubeVirtConfiguration{
						VirtioFSConfiguration: &v1.VirtioFSConfiguration{
							CachingPolicy: entry.cachePolicy,
						},
					},
				},
			})
		}
		vmi := api.NewMinimalVMI("testvm")

		vmi.Spec.Volumes = append(vmi.Spec.Volumes, v1.Volume{
			Name: "sharedtestdisk",
			VolumeSource: v1.VolumeSource{
				PersistentVolumeClaim: testutils.NewFakePersistentVolumeSource(),
			},
		})
		vmi.Spec.Domain.Devices.Filesystems = append(vmi.Spec.Domain.Devices.Filesystems, v1.Filesystem{
			Name:     "sharedtestdisk",
			Virtiofs: &v1.FilesystemVirtiofs{},
		})

		vmi.Spec.Volumes = append(vmi.Spec.Volumes, v1.Volume{
			Name: "secret-volume",
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: "test-secret",
				},
			},
		})
		vmi.Spec.Domain.Devices.Filesystems = append(vmi.Spec.Domain.Devices.Filesystems, v1.Filesystem{
			Name:     "secret-volume",
			Virtiofs: &v1.FilesystemVirtiofs{},
		})

		container := generateVirtioFSContainers(vmi, "virtiofs-container", config)
		Expect(container).To(HaveLen(2))

		expectedCachePolicy := virtconfig.DefaultVirtioFSCachingPolicy
		if entry.cachePolicy != nil {
			expectedCachePolicy = *entry.cachePolicy
		}
		// PV
		Expect(container[0].SecurityContext.RunAsNonRoot).To(HaveValue(BeTrue()))
		Expect(container[0].SecurityContext.AllowPrivilegeEscalation).To(HaveValue(BeFalse()))
		Expect(container[0].Args).To(ContainElement(fmt.Sprintf("--cache=%s", expectedCachePolicy)))
		// Secret
		Expect(container[1].SecurityContext.RunAsNonRoot).To(HaveValue(BeTrue()))
		Expect(container[1].SecurityContext.AllowPrivilegeEscalation).To(HaveValue(BeFalse()))
		Expect(container[1].Args).To(ContainElement(fmt.Sprintf("--cache=%s", expectedCachePolicy)))
	},
		Entry("with CachePolicy never", testEntry{cachePolicy: pointer.P(v1.VirtioFSCachingPolicyNever)}),
		Entry("with CachePolicy auto", testEntry{cachePolicy: pointer.P(v1.VirtioFSCachingPolicyAuto)}),
		Entry("with empty CachePolicy", testEntry{cachePolicy: nil}),
	)
})
