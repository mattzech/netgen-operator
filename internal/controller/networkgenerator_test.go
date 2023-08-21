package controller

import (
	"context"
	//"fmt"
	"time"
	//monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	v1alpha1 "github.com/aws-controllers-k8s/ec2-controller/apis/v1alpha1"
	//ackv1alpha1 "github.com/aws-controllers-k8s/runtime/apis/core/v1alpha1"
	kosmosv1 "github.com/mattzech/netgen-operator/api/v1"
	//"honnef.co/go/tools/simple"

	//appsv1 "k8s.io/api/apps/v1"
	//autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	//netv1 "k8s.io/api/networking/v1"
	//policyv1 "k8s.io/api/policy/v1"
	//k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	//"k8s.io/client-go/kubernetes"
	//ctrl "sigs.k8s.io/controller-runtime"
	//"sigs.k8s.io/controller-runtime/pkg/client"
	//"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	//"strings"
	//"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/testernetes/gkube"
)

var _ = Describe("NetworkGeneratorOperator: Simple NetworkGen lifecycle", Ordered, func() {

	var (
		namespace *corev1.Namespace
		ng        *kosmosv1.NetworkGenerator

		ngName          = "little-netgen"
		originNamespace = "simple"
		nameKey         = "Name"
		typeKey         = "Type"
	)

	originCIDR := make([]string, 1)
	originCIDR[0] = "30.0.0.0/23"

	selectedVPCE := make([]string, 2)
	selectedVPCE[0] = "sqs"
	selectedVPCE[1] = "dynamodb"
	tagKey := "Environment"
	tagValue := "engineering"

	AfterAll(func(ctx context.Context) {
		Eventually(k8s.Delete).WithContext(ctx).WithArguments(namespace).Should(Succeed())
		Eventually(k8s.Delete).WithContext(ctx).WithArguments(ng).Should(Succeed())
	})

	Describe("creating NetworkGen and subresources", Ordered, func() {
		BeforeAll(func(ctx context.Context) {
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: originNamespace,
				},
			}
			ng = &kosmosv1.NetworkGenerator{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ngName,
					Namespace: originNamespace,
				},
				Spec: kosmosv1.NetworkGeneratorSpec{
					CIDRBlocks: []*string{
						&originCIDR[0],
					},
					Region:       "us-east-1",
					Environment:  "engineering",
					SubnetDesign: "gkop-new",
					VPCEndpoints: selectedVPCE,
					Tags: []*v1alpha1.Tag{
						{
							Key:   &tagKey,
							Value: &tagValue,
						},
					},
				},
			}
			Eventually(k8s.Create).WithContext(ctx).WithArguments(namespace).Should(Succeed())
			Eventually(k8s.Create).WithContext(ctx).WithArguments(ng).Should(Succeed())
		})

		When("creating a simple NetworkGen with an empty spec", func() {
			It("should create a VPC in the same Namespace", func(ctx context.Context) {
				simpleVPC := &v1alpha1.VPC{ObjectMeta: metav1.ObjectMeta{
					Name:      "engineering",
					Namespace: "simple",
				}}

				Eventually(k8s.Get).WithContext(ctx).WithArguments(simpleVPC).Should(Succeed())
				Eventually(k8s.Object).WithContext(ctx).WithArguments(simpleVPC).Should(HaveJSONPath(`{.spec.cidrBlocks[0]}`, Equal(&originCIDR[0])))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(simpleVPC).Should(HaveJSONPath(`{.spec.tags[0].key}`, Equal(&tagKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(simpleVPC).Should(HaveJSONPath(`{.spec.tags[0].value}`, Equal(&tagValue)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(simpleVPC).Should(HaveJSONPath(`{.spec.tags[1].key}`, Equal(&nameKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(simpleVPC).Should(HaveJSONPath(`{.spec.tags[1].value}`, Equal(&simpleVPC.Name)))
			}, SpecTimeout(time.Second*60))

			It("should create an InternetGateway in the same Namespace", func(ctx context.Context) {

				ig := &v1alpha1.InternetGateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ng.Spec.Environment + "-vpc",
						Namespace: ng.Namespace,
					},
				}

				Eventually(k8s.Get).WithContext(ctx).WithArguments(ig).Should(Succeed())
				Eventually(k8s.Object).WithContext(ctx).WithArguments(ig).Should(HaveJSONPath(`{.spec.vpcRef.from.name}`, Equal(pointer(ng.Spec.Environment))))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(ig).Should(HaveJSONPath(`{.spec.tags[0].key}`, Equal(&tagKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(ig).Should(HaveJSONPath(`{.spec.tags[0].value}`, Equal(&tagValue)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(ig).Should(HaveJSONPath(`{.spec.tags[1].key}`, Equal(&nameKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(ig).Should(HaveJSONPath(`{.spec.tags[1].value}`, Equal(&ig.Name)))
			}, SpecTimeout(time.Second*60))

			It("should find each type of Subnets created", func(ctx context.Context) {
				sn_private := &v1alpha1.Subnet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ng.Spec.Environment + "-vpc-private-us-east-1a",
						Namespace: ng.Namespace,
					},
				}
				sn_public := &v1alpha1.Subnet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ng.Spec.Environment + "-vpc-public-us-east-1b",
						Namespace: ng.Namespace,
					},
				}
				sn_transit := &v1alpha1.Subnet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ng.Spec.Environment + "-vpc-transit-us-east-1a",
						Namespace: ng.Namespace,
					},
				}

				Eventually(k8s.Get).WithContext(ctx).WithArguments(sn_private).Should(Succeed())
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_private).Should(HaveJSONPath(`{.spec.vpcRef.from.name}`, Equal(pointer(ng.Spec.Environment))))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_private).Should(HaveJSONPath(`{.spec.routeTableRefs[0].from.name}`, Equal(pointer(sn_private.Name))))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_private).Should(HaveJSONPath(`{.spec.tags[0].key}`, Equal(&tagKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_private).Should(HaveJSONPath(`{.spec.tags[0].value}`, Equal(&tagValue)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_private).Should(HaveJSONPath(`{.spec.tags[1].key}`, Equal(&nameKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_private).Should(HaveJSONPath(`{.spec.tags[1].value}`, Equal(&sn_private.Name)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_private).Should(HaveJSONPath(`{.spec.tags[2].key}`, Equal(&typeKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_private).Should(HaveJSONPath(`{.spec.tags[2].value}`, Equal(pointer("private"))))
				Eventually(k8s.Get).WithContext(ctx).WithArguments(sn_public).Should(Succeed())
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_public).Should(HaveJSONPath(`{.spec.vpcRef.from.name}`, Equal(pointer(ng.Spec.Environment))))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_public).Should(HaveJSONPath(`{.spec.routeTableRefs[0].from.name}`, Equal(pointer(ng.Spec.Environment+"-vpc-public"))))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_public).Should(HaveJSONPath(`{.spec.tags[0].key}`, Equal(&tagKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_public).Should(HaveJSONPath(`{.spec.tags[0].value}`, Equal(&tagValue)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_public).Should(HaveJSONPath(`{.spec.tags[1].key}`, Equal(&nameKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_public).Should(HaveJSONPath(`{.spec.tags[1].value}`, Equal(&sn_public.Name)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_public).Should(HaveJSONPath(`{.spec.tags[2].key}`, Equal(&typeKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_public).Should(HaveJSONPath(`{.spec.tags[2].value}`, Equal(pointer("public"))))
				Eventually(k8s.Get).WithContext(ctx).WithArguments(sn_transit).Should(Succeed())
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_transit).Should(HaveJSONPath(`{.spec.vpcRef.from.name}`, Equal(pointer(ng.Spec.Environment))))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_transit).Should(HaveJSONPath(`{.spec.routeTableRefs[0].from.name}`, Equal(pointer(sn_transit.Name))))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_transit).Should(HaveJSONPath(`{.spec.tags[0].key}`, Equal(&tagKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_transit).Should(HaveJSONPath(`{.spec.tags[0].value}`, Equal(&tagValue)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_transit).Should(HaveJSONPath(`{.spec.tags[1].key}`, Equal(&nameKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_transit).Should(HaveJSONPath(`{.spec.tags[1].value}`, Equal(&sn_transit.Name)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_transit).Should(HaveJSONPath(`{.spec.tags[2].key}`, Equal(&typeKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sn_transit).Should(HaveJSONPath(`{.spec.tags[2].value}`, Equal(pointer("transit"))))
			}, SpecTimeout(time.Second*60))

			It("should create a public RouteTable in the same Namespace with correct Route", func(ctx context.Context) {
				simpleVPC := &v1alpha1.VPC{ObjectMeta: metav1.ObjectMeta{
					Name:      "engineering",
					Namespace: "simple",
				}}
				rt_public := &v1alpha1.RouteTable{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ng.Spec.Environment + "-vpc-public",
						Namespace: ng.Namespace,
					},
				}

				Eventually(k8s.Get).WithContext(ctx).WithArguments(rt_public).Should(Succeed())
				Eventually(k8s.Object).WithContext(ctx).WithArguments(rt_public).Should(HaveJSONPath(`{.spec.vpcRef.from.name}`, Equal(pointer(simpleVPC.Name))))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(rt_public).Should(HaveJSONPath(`{.spec.routes[0].gatewayRef.from.name}`, Equal(pointer("engineering-vpc"))))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(rt_public).Should(HaveJSONPath(`{.spec.tags[0].key}`, Equal(&tagKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(rt_public).Should(HaveJSONPath(`{.spec.tags[0].value}`, Equal(&tagValue)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(rt_public).Should(HaveJSONPath(`{.spec.tags[1].key}`, Equal(&nameKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(rt_public).Should(HaveJSONPath(`{.spec.tags[1].value}`, Equal(&rt_public.Name)))
			}, SpecTimeout(time.Second*60))

			It("should create a private RouteTable in the same Namespace with correct Route", func(ctx context.Context) {
				simpleVPC := &v1alpha1.VPC{ObjectMeta: metav1.ObjectMeta{
					Name:      "engineering",
					Namespace: "simple",
				}}
				rt_private := &v1alpha1.RouteTable{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ng.Spec.Environment + "-vpc-private-us-east-1a",
						Namespace: ng.Namespace,
					},
				}

				Eventually(k8s.Get).WithContext(ctx).WithArguments(rt_private).Should(Succeed())
				Eventually(k8s.Object).WithContext(ctx).WithArguments(rt_private).Should(HaveJSONPath(`{.spec.vpcRef.from.name}`, Equal(pointer(simpleVPC.Name))))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(rt_private).Should(HaveJSONPath(`{.spec.routes[0].gatewayRef.from.name}`, Equal(pointer("engineering-vpc-us-east-1a"))))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(rt_private).Should(HaveJSONPath(`{.spec.tags[0].key}`, Equal(&tagKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(rt_private).Should(HaveJSONPath(`{.spec.tags[0].value}`, Equal(&tagValue)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(rt_private).Should(HaveJSONPath(`{.spec.tags[1].key}`, Equal(&nameKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(rt_private).Should(HaveJSONPath(`{.spec.tags[1].value}`, Equal(&rt_private.Name)))
			}, SpecTimeout(time.Second*60))

			It("should create a transit RouteTable in the same Namespace", func(ctx context.Context) {
				simpleVPC := &v1alpha1.VPC{ObjectMeta: metav1.ObjectMeta{
					Name:      "engineering",
					Namespace: "simple",
				}}
				rt_transit := &v1alpha1.RouteTable{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ng.Spec.Environment + "-vpc-transit-us-east-1a",
						Namespace: ng.Namespace,
					},
				}

				Eventually(k8s.Get).WithContext(ctx).WithArguments(rt_transit).Should(Succeed())
				Eventually(k8s.Object).WithContext(ctx).WithArguments(rt_transit).Should(HaveJSONPath(`{.spec.vpcRef.from.name}`, Equal(pointer(simpleVPC.Name))))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(rt_transit).Should(HaveJSONPath(`{.spec.tags[0].key}`, Equal(&tagKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(rt_transit).Should(HaveJSONPath(`{.spec.tags[0].value}`, Equal(&tagValue)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(rt_transit).Should(HaveJSONPath(`{.spec.tags[1].key}`, Equal(&nameKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(rt_transit).Should(HaveJSONPath(`{.spec.tags[1].value}`, Equal(&rt_transit.Name)))
			}, SpecTimeout(time.Second*60))

			It("should create an ElasticIPaddress in the same Namespace", func(ctx context.Context) {
				eip := &v1alpha1.ElasticIPAddress{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ng.Spec.Environment + "-vpc-us-east-1a",
						Namespace: ng.Namespace,
					},
				}

				Eventually(k8s.Get).WithContext(ctx).WithArguments(eip).Should(Succeed())
			}, SpecTimeout(time.Second*60))

			It("should create a NAT Gateway in the same Namespace", func(ctx context.Context) {
				subnetRefName := ng.Spec.Environment + "-vpc-public-us-east-1a"
				nat := &v1alpha1.NATGateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      ng.Spec.Environment + "-vpc-us-east-1a",
						Namespace: ng.Namespace,
					},
				}

				Eventually(k8s.Get).WithContext(ctx).WithArguments(nat).Should(Succeed())
				Eventually(k8s.Object).WithContext(ctx).WithArguments(nat).Should(HaveJSONPath(`{.spec.subnetRef.from.name}`, Equal(pointer(subnetRefName))))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(nat).Should(HaveJSONPath(`{.spec.allocationRef.from.name}`, Equal(pointer(nat.Name))))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(nat).Should(HaveJSONPath(`{.spec.tags[0].key}`, Equal(&tagKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(nat).Should(HaveJSONPath(`{.spec.tags[0].value}`, Equal(&tagValue)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(nat).Should(HaveJSONPath(`{.spec.tags[1].key}`, Equal(&nameKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(nat).Should(HaveJSONPath(`{.spec.tags[1].value}`, Equal(&nat.Name)))
			}, SpecTimeout(time.Second*60))

			It("should create a VPC Endpoint SQS in the same Namespace", func(ctx context.Context) {
				sqs := &v1alpha1.VPCEndpoint{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vpc-endpoint-sqs-engineering",
						Namespace: ng.Namespace,
					},
				}

				Eventually(k8s.Get).WithContext(ctx).WithArguments(sqs).Should(Succeed())
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sqs).Should(HaveJSONPath(`{.spec.vpcEndpointType}`, Equal(pointer("Interface"))))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sqs).Should(HaveJSONPath(`{.spec.tags[0].key}`, Equal(&tagKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sqs).Should(HaveJSONPath(`{.spec.tags[0].value}`, Equal(&tagValue)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sqs).Should(HaveJSONPath(`{.spec.tags[1].key}`, Equal(&nameKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(sqs).Should(HaveJSONPath(`{.spec.tags[1].value}`, Equal(&sqs.Name)))
			}, SpecTimeout(time.Second*60))

			It("should create a VPC Endpoint for DynamoDB in the same Namespace", func(ctx context.Context) {
				dynamoDB := &v1alpha1.VPCEndpoint{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vpc-endpoint-dynamodb-engineering",
						Namespace: ng.Namespace,
					},
				}

				Eventually(k8s.Get).WithContext(ctx).WithArguments(dynamoDB).Should(Succeed())
				Eventually(k8s.Object).WithContext(ctx).WithArguments(dynamoDB).Should(HaveJSONPath(`{.spec.vpcEndpointType}`, Equal(pointer("Gateway"))))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(dynamoDB).Should(HaveJSONPath(`{.spec.tags[0].key}`, Equal(&tagKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(dynamoDB).Should(HaveJSONPath(`{.spec.tags[0].value}`, Equal(&tagValue)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(dynamoDB).Should(HaveJSONPath(`{.spec.tags[1].key}`, Equal(&nameKey)))
				Eventually(k8s.Object).WithContext(ctx).WithArguments(dynamoDB).Should(HaveJSONPath(`{.spec.tags[1].value}`, Equal(&dynamoDB.Name)))
			}, SpecTimeout(time.Second*60))
		})

		// Deletion is a part of the controller manager logic - need way to test this. Confirmed to be working when running `make run` in kind cluster

		// When("the NetworkGenerator is deleted", func() {
		// 	It("should delete the NetworkGenerator successfully", func(ctx context.Context) {
		// 		Eventually(k8s.Delete).WithContext(ctx).WithArguments(ng).Should(Succeed())
		// 	}, SpecTimeout(time.Second*60))
		// 	It("should not find the NetworkGenerator after it is deleted", func(ctx context.Context) {
		// 		Eventually(k8s.Get).WithContext(ctx).WithArguments(ng).ShouldNot(Succeed())
		// 	}, SpecTimeout(time.Second*60))
		// 	It("should delete the VPC", func(ctx context.Context) {
		// 		Eventually(k8s.Delete).WithContext(ctx).WithArguments(&v1alpha1.VPC{
		// 			ObjectMeta: metav1.ObjectMeta{
		// 				Name:      "engineering",
		// 				Namespace: ng.Namespace,
		// 			}}).Should(Succeed())
		// 	}, SpecTimeout(time.Second*60))
		// 	It("should not the VPC", func(ctx context.Context) {
		// 		Eventually(k8s.Get).WithContext(ctx).WithArguments(&v1alpha1.VPC{
		// 			ObjectMeta: metav1.ObjectMeta{
		// 				Name:      "engineering",
		// 				Namespace: ng.Namespace,
		// 			}}).ShouldNot(Succeed())
		// 	}, SpecTimeout(time.Second*60))
		// })
	})
})
