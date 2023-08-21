/*
Copyright 2023 Atlas.
*/

package controller

import (
	"context"
	//"encoding/json"
	"fmt"

	"strings"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/go-logr/logr"
	"github.com/kr/pretty"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/strings/slices"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logger "sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/aws-controllers-k8s/ec2-controller/apis/v1alpha1"
	ackv1alpha1 "github.com/aws-controllers-k8s/runtime/apis/core/v1alpha1"

	//ackerrors "github.com/aws-controllers-k8s/runtime/pkg/errors"
	//acktypes "github.com/aws-controllers-k8s/runtime/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kosmosv1 "github.com/mattzech/netgen-operator/api/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
)

// NetworkGeneratorReconciler reconciles a NetworkGenerator object
type NetworkGeneratorReconciler struct {
	client.Client
	EC2Client *ec2.Client
	Scheme    *runtime.Scheme
}

// +kubebuilder:rbac:groups=kosmos.atlas.fis.dev,resources=networkgenerators,verbs=*
// +kubebuilder:rbac:groups=kosmos.atlas.fis.dev,resources=networkgenerators/status,verbs=*
// +kubebuilder:rbac:groups=kosmos.atlas.fis.dev,resources=networkgenerators/finalizers,verbs=*
// +kubebuilder:rbac:groups=ec2.services.k8s.aws,resources=vpcs;subnets;natgateways;internetgateways;routetables;elasticipaddresses;vpcendpoints,verbs=*
// +kubebuilder:rbac:groups=services.k8s.aws,resources=adoptedresources,verbs=*
// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the NetworkGenerator object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *NetworkGeneratorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logger.FromContext(ctx)
	var op controllerutil.OperationResult
	az := [3]string{"a", "b", "c"}
	ng := &kosmosv1.NetworkGenerator{}
	err := r.Get(ctx, req.NamespacedName, ng)
	if err != nil {
		if k8sErrors.IsNotFound(err) {
			log.Info("Request object not found, could have been deleted after reconcile request.")
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get NetworkGenerator.")
		return ctrl.Result{}, err
	}

	// Design Subnets based on subnet design Spec
	var subnetList []string
	subnet_type := make([]string, 3)
	if ng.Spec.SubnetDesign == "gkop-new" {
		subnetList = gkopNewGetSubnets(*ng.Spec.CIDRBlocks[0])
		subnet_type[0] = "public"
		subnet_type[1] = "private"
		subnet_type[2] = "transit"
	} else if ng.Spec.SubnetDesign == "gkop-legacy" {
		subnetList = gkopLegacyGetSubnets(*ng.Spec.CIDRBlocks[0])
		subnet_type[0] = "public"
		subnet_type[1] = "private"
		subnet_type[2] = "service"
		subnet_type = append(subnet_type, "transit")

	} else {
		return ctrl.Result{}, errors.Wrap(err, "Spec.SubnetDesign: value not acceptable")
	}

	var kind string
	// Reconcile the ExistingAWSResources field in order to begin managing these resources with ACK
	for _, orphan := range ng.Spec.ExistingAWSResources {

		// Create adopted resource if existing resouce is a VPC
		if strings.ToLower(orphan.Kind) == "vpc" {
			// vpc ipv4CIDR should match exactly the NetworkGenerator "CIDRBlocks" field
			if orphan.IPv4CIDR == *ng.Spec.CIDRBlocks[0] {
				kind = "VPC"
				newAdoptedResource := &ackv1alpha1.AdoptedResource{
					ObjectMeta: metav1.ObjectMeta{
						Name:      orphan.Name,
						Namespace: ng.Namespace,
					},
				}
				op, err = ctrl.CreateOrUpdate(ctx, r.Client, newAdoptedResource, func() error {
					orig := newAdoptedResource.DeepCopy()
					expectedAdoptedResource := r.adoptedResourceForNetworkGen(pointer(orphan), ng, kind)
					newAdoptedResource.Spec = expectedAdoptedResource.Spec

					printIfDiff(log, orig, newAdoptedResource)
					return nil // Manual watch for cross namespace resource
				})
				if err != nil {
					return ctrl.Result{}, errors.Wrap(err, "Failed to reconcile VPC AdoptedResource.")
				}
				log.V(1).Info("Successfully reconciled ExistingAWSResource.", "operation", op)
			} else {
				return ctrl.Result{}, errors.Wrap(err, "Existing VPC CIDR needs to match NetworkGenerator CIDRBocks[]")
			}

		}

		// Reconcile Existing InternetGatewway
		if strings.ToLower(orphan.Kind) == "internetgateway" {
			kind = "InternetGateway"
			newAdoptedResource := &ackv1alpha1.AdoptedResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      orphan.Name,
					Namespace: ng.Namespace,
				},
			}
			op, err = ctrl.CreateOrUpdate(ctx, r.Client, newAdoptedResource, func() error {
				orig := newAdoptedResource.DeepCopy()
				expectedAdoptedResource := r.adoptedResourceForNetworkGen(pointer(orphan), ng, kind)
				newAdoptedResource.Spec = expectedAdoptedResource.Spec

				printIfDiff(log, orig, newAdoptedResource)
				return nil // Manual watch for cross namespace resource
			})
			if err != nil {
				return ctrl.Result{}, errors.Wrap(err, "Failed to reconcile InternetGateway AdoptedResource.")
			}
			log.V(1).Info("Successfully reconciled ExistingAWSResource.", "operation", op)
		}

		// Reconcile Existing RouteTables
		if strings.ToLower(orphan.Kind) == "routetable" {
			kind = "RouteTable"
			newAdoptedResource := &ackv1alpha1.AdoptedResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      orphan.Name,
					Namespace: ng.Namespace,
				},
			}
			op, err = ctrl.CreateOrUpdate(ctx, r.Client, newAdoptedResource, func() error {
				orig := newAdoptedResource.DeepCopy()
				expectedAdoptedResource := r.adoptedResourceForNetworkGen(pointer(orphan), ng, kind)
				newAdoptedResource.Spec = expectedAdoptedResource.Spec

				printIfDiff(log, orig, newAdoptedResource)
				return nil // Manual watch for cross namespace resource
			})
			if err != nil {
				return ctrl.Result{}, errors.Wrap(err, "Failed to reconcile RouteTable AdoptedResource.")
			}
			log.V(1).Info("Successfully reconciled ExistingAWSResource.", "operation", op)
		}

		// If existing resource is a subnet, first check if it fits with the subnet ranges.
		// Then, create an AdoptedResource of kind Subnet
		if strings.ToLower(orphan.Kind) == "subnet" {
			isValidCIDR := slices.Contains(subnetList, orphan.IPv4CIDR)
			kind = "Subnet"
			if isValidCIDR {
				// Remove CIDR from list to avoid duplicate subnet CIDRs
				for i, cidr := range subnetList {
					if cidr == orphan.IPv4CIDR {
						subnetList[i] = "nil"
					}
				}
				newAdoptedResource := &ackv1alpha1.AdoptedResource{
					ObjectMeta: metav1.ObjectMeta{
						Name:      orphan.Name,
						Namespace: ng.Namespace,
					},
				}
				op, err = ctrl.CreateOrUpdate(ctx, r.Client, newAdoptedResource, func() error {
					orig := newAdoptedResource.DeepCopy()
					expectedAdoptedResource := r.adoptedResourceForNetworkGen(pointer(orphan), ng, kind)
					newAdoptedResource.Spec = expectedAdoptedResource.Spec

					printIfDiff(log, orig, newAdoptedResource)
					return nil // Manual watch for cross namespace resource
				})
				if err != nil {
					return ctrl.Result{}, errors.Wrap(err, "Failed to reconcile Subnet AdoptedResource.")
				}
				log.V(1).Info("Successfully reconciled ExistingAWSResource.", "operation", op)
			} else {
				return ctrl.Result{}, errors.Wrap(err, "Duplicate CIDR found in ExistingAWSResources.")
			}
		}

		// Reconcile Existing ElasticIPs
		if strings.ToLower(orphan.Kind) == "elasticipaddress" {
			kind = "ElasticIPAddress"
			newAdoptedResource := &ackv1alpha1.AdoptedResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      orphan.Name,
					Namespace: ng.Namespace,
				},
			}
			op, err = ctrl.CreateOrUpdate(ctx, r.Client, newAdoptedResource, func() error {
				orig := newAdoptedResource.DeepCopy()
				expectedAdoptedResource := r.adoptedResourceForNetworkGen(pointer(orphan), ng, kind)
				newAdoptedResource.Spec = expectedAdoptedResource.Spec

				printIfDiff(log, orig, newAdoptedResource)
				return nil // Manual watch for cross namespace resource
			})
			if err != nil {
				return ctrl.Result{}, errors.Wrap(err, "Failed to reconcile ElasticIPAddress AdoptedResource.")
			}
			log.V(1).Info("Successfully reconciled ExistingAWSResource.", "operation", op)
		}

		if strings.ToLower(orphan.Kind) == "natgateway" {
			kind = "NATGateway"
			newAdoptedResource := &ackv1alpha1.AdoptedResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      orphan.Name,
					Namespace: ng.Namespace,
				},
			}
			op, err = ctrl.CreateOrUpdate(ctx, r.Client, newAdoptedResource, func() error {
				orig := newAdoptedResource.DeepCopy()
				expectedAdoptedResource := r.adoptedResourceForNetworkGen(pointer(orphan), ng, kind)
				newAdoptedResource.Spec = expectedAdoptedResource.Spec

				printIfDiff(log, orig, newAdoptedResource)
				return nil // Manual watch for cross namespace resource
			})
			if err != nil {
				return ctrl.Result{}, errors.Wrap(err, "Failed to reconcile NATGateway AdoptedResource.")
			}
			log.V(1).Info("Successfully reconciled ExistingAWSResource.", "operation", op)
		}

		// if strings.ToLower(parsedArn.Resource) == "transitgateway" {
		// }

	}

	// Reconcile VPC
	vpc := &v1alpha1.VPC{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ng.Spec.Environment,
			Namespace: ng.Namespace},
	}
	op, err = ctrl.CreateOrUpdate(ctx, r.Client, vpc, func() error {
		orig := vpc.DeepCopy()
		expectedVpc := r.vpcForNetworkGen(ng)
		vpc.Spec = expectedVpc.Spec

		if transitgatewayID, connect := vpc.Annotations["accept.gkop.io"]; connect {
			// check status to see if connection already established, return

			output, err := r.EC2Client.CreateTransitGatewayVpcAttachment(ctx, &ec2.CreateTransitGatewayVpcAttachmentInput{
				SubnetIds:        []string{},
				TransitGatewayId: pointer(transitgatewayID),
				VpcId:            pointer(""),
			})
			if err != nil {
				return err
			}
			//check output

			_ = output
		}

		printIfDiff(log, orig, vpc)
		// Set controller to own VPC
		err = ctrl.SetControllerReference(ng, vpc, r.Scheme)
		return err
	})
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "Failed to reconcile VPC.")
	}
	log.V(1).Info("Successfully reconciled VPC.", "operation", op)

	// Reconcile InternetGateway
	ig := &v1alpha1.InternetGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ng.Spec.Environment + "-vpc",
			Namespace: ng.Namespace},
	}
	op, err = ctrl.CreateOrUpdate(ctx, r.Client, ig, func() error {
		orig := ig.DeepCopy()
		expectedIg := r.igForNetworkGen(ng, vpc)
		ig.Spec = expectedIg.Spec

		printIfDiff(log, orig, ig)
		// Set controller to own InternetGateway
		err = ctrl.SetControllerReference(ng, ig, r.Scheme)
		return err
	})
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "Failed to reconcile InternetGateway.")
	}

	log.V(1).Info("Successfully reconciled InternetGateway.", "operation", op)

	// Reconcile RouteTable
	// Need 1 per AZ for private and transit
	// Just need 1 for public
	for _, sub_type := range subnet_type {
		// Make 3 route tables (one for each AZ) for private and transit
		if sub_type == "private" {
			for _, zone := range az {
				long_name := ng.Spec.Environment + "-vpc-" + sub_type + "-" + ng.Spec.Region + zone
				routetable := &v1alpha1.RouteTable{
					ObjectMeta: metav1.ObjectMeta{
						Name:      long_name,
						Namespace: ng.Namespace,
					},
				}
				op, err = ctrl.CreateOrUpdate(ctx, r.Client, routetable, func() error {
					orig := routetable.DeepCopy()
					expectedRouteTable := r.privateRouteTableForNetworkGen(ng, vpc, long_name, ng.Spec.Region, zone)
					routetable.Spec = expectedRouteTable.Spec

					printIfDiff(log, orig, routetable)
					// Set controller to own RouteTable
					err = ctrl.SetControllerReference(ng, routetable, r.Scheme)
					return err
				})
				if err != nil {
					return ctrl.Result{}, errors.Wrap(err, "Failed to reconcile RouteTable.")
				}

				log.V(1).Info("Successfully reconciled RouteTable.", "operation", op)
			}
		}
		if sub_type == "transit" {
			for _, zone := range az {
				long_name := ng.Spec.Environment + "-vpc-" + sub_type + "-" + ng.Spec.Region + zone
				routetable := &v1alpha1.RouteTable{
					ObjectMeta: metav1.ObjectMeta{
						Name:      long_name,
						Namespace: ng.Namespace,
					},
				}
				op, err = ctrl.CreateOrUpdate(ctx, r.Client, routetable, func() error {
					orig := routetable.DeepCopy()
					expectedRouteTable := r.emptyRouteTableForNetworkGen(ng, vpc, long_name, ng.Spec.Region, zone)
					routetable.Spec = expectedRouteTable.Spec

					printIfDiff(log, orig, routetable)
					// Set controller to own RouteTable
					err = ctrl.SetControllerReference(ng, routetable, r.Scheme)
					return err
				})
				if err != nil {
					return ctrl.Result{}, errors.Wrap(err, "Failed to reconcile RouteTable.")
				}

				log.V(1).Info("Successfully reconciled RouteTable.", "operation", op)
			}
		}
		if sub_type == "public" {
			long_name := ng.Spec.Environment + "-vpc-" + sub_type
			routetable := &v1alpha1.RouteTable{
				ObjectMeta: metav1.ObjectMeta{
					Name:      long_name,
					Namespace: ng.Namespace,
				},
			}
			op, err = ctrl.CreateOrUpdate(ctx, r.Client, routetable, func() error {
				orig := routetable.DeepCopy()
				expectedRouteTable := r.publicRouteTableForNetworkGen(ng, vpc, long_name, ng.Spec.Region, "")
				routetable.Spec = expectedRouteTable.Spec

				printIfDiff(log, orig, routetable)
				// Set controller to own RouteTable
				err = ctrl.SetControllerReference(ng, routetable, r.Scheme)
				return err
			})
			if err != nil {
				return ctrl.Result{}, errors.Wrap(err, "Failed to reconcile RouteTable.")
			}
			log.V(1).Info("Successfully reconciled RouteTable.", "operation", op)
		}
		// Do not need anything for service
		if sub_type == "service" {
		}
	}

	// Reconcile Subnet
	// Need 1 subnet per AZ (3 total) for each of private, public, transit
	// 9 total
	count := 0
	var rt_name string
	for _, sub_type := range subnet_type {
		for _, zone := range az {
			// Map subnets to the correct RouteTables as shown in AWS console
			if sub_type == "public" {
				rt_name = ng.Spec.Environment + "-vpc-" + sub_type
			}
			if (sub_type == "service") || (sub_type == "private") {
				rt_name = ng.Spec.Environment + "-vpc-private-" + ng.Spec.Region + zone
			}
			if sub_type == "transit" {
				rt_name = ng.Spec.Environment + "-vpc-" + sub_type + "-" + ng.Spec.Region + zone
			}
			// Reconcile subnet name
			long_name := ng.Spec.Environment + "-vpc-" + sub_type + "-" + ng.Spec.Region + zone
			subnet := &v1alpha1.Subnet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      long_name,
					Namespace: ng.Namespace,
				},
			}
			op, err = ctrl.CreateOrUpdate(ctx, r.Client, subnet, func() error {
				orig := subnet.DeepCopy()
				expectedSubnet := r.subnetForNetworkGen(ng, vpc, subnet, rt_name, subnetList[count], ng.Spec.Region, zone, sub_type)
				subnet.Spec = expectedSubnet.Spec

				printIfDiff(log, orig, subnet)
				// Set controller to own Subnet
				err = ctrl.SetControllerReference(ng, subnet, r.Scheme)
				return err
			})
			if err != nil {
				return ctrl.Result{}, errors.Wrap(err, "Failed to reconcile Subnet "+long_name)
			}
			log.V(1).Info("Successfully reconciled Subnet.", "operation", op)
			count++
		}
	}

	// Need Elastic IP first for NatGateway to reference
	for _, zone := range az {
		eipName := ng.Spec.Environment + "-vpc-" + ng.Spec.Region + zone
		elasticIP := &v1alpha1.ElasticIPAddress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      eipName,
				Namespace: ng.Namespace,
			},
		}
		op, err = ctrl.CreateOrUpdate(ctx, r.Client, elasticIP, func() error {
			orig := elasticIP.DeepCopy()
			expectedelasticIP := r.elasticIPForNetworkGen(ng, eipName)
			elasticIP.Spec = expectedelasticIP.Spec

			printIfDiff(log, orig, elasticIP)
			// Set controller to own Elastic IP
			err = ctrl.SetControllerReference(ng, elasticIP, r.Scheme)
			return err
		})
		if err != nil {
			return ctrl.Result{}, errors.Wrap(err, "Failed to reconcile ElasticIPAddress.")
		}
	}

	// Reconcile NAT Gateway
	for _, zone := range az {
		long_name := ng.Spec.Environment + "-vpc-" + ng.Spec.Region + zone
		subnet_name := ng.Spec.Environment + "-vpc-public-" + ng.Spec.Region + zone
		natGateway := &v1alpha1.NATGateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      long_name,
				Namespace: ng.Namespace,
			},
		}
		op, err = ctrl.CreateOrUpdate(ctx, r.Client, natGateway, func() error {
			orig := natGateway.DeepCopy()
			expectedNatGateway := r.natGatewayForNetworkGen(ng, subnet_name, long_name)
			natGateway.Spec = expectedNatGateway.Spec

			printIfDiff(log, orig, natGateway)
			// Set controller to own NAT Gateway
			err = ctrl.SetControllerReference(ng, natGateway, r.Scheme)
			return err
		})
		if err != nil {
			return ctrl.Result{}, errors.Wrap(err, "Failed to reconcile NATGateway.")
		}

		log.V(1).Info("Successfully reconciled NATGateway.", "operation", op)
	}

	// Reconcile VPC Endpoints
	for _, service := range ng.Spec.VPCEndpoints {
		service = strings.ToLower(service)
		if (service == "kms") || (service == "sqs") || (service == "secretsmanager") || (service == "s3") || (service == "dynamodb") {
			name := "vpc-endpoint-" + service + "-" + vpc.Name
			vpce := &v1alpha1.VPCEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ng.Namespace,
				},
			}
			op, err = ctrl.CreateOrUpdate(ctx, r.Client, vpce, func() error {
				orig := vpce.DeepCopy()
				expectedVpcEndpoint := r.vpcEndpointforNetworkGen(ng, vpc, service, name)
				vpce.Spec = expectedVpcEndpoint.Spec

				printIfDiff(log, orig, vpce)
				// Set controller to own RouteTable
				err = ctrl.SetControllerReference(ng, vpce, r.Scheme)
				return err
			})
			if err != nil {
				return ctrl.Result{}, errors.Wrap(err, "Failed to reconcile VPCEndpoint.")
			}
			log.V(1).Info("Successfully reconciled VPC Endpoint.", "operation", op)
		}
	}

	// Reconcile VPC again if annotation has been applied.
	// Needs to be at the end so we can get the IDs of Subnets
	op, err = ctrl.CreateOrUpdate(ctx, r.Client, vpc, func() error {
		orig := vpc.DeepCopy()
		expectedVpc := r.vpcForNetworkGen(ng)
		vpc.Spec = expectedVpc.Spec

		// if VPC has an annotation with the key "accepted.transitgatewayID.gkop.io" - assign the value to var transitgatewayID
		if transitgatewayID, connect := vpc.Annotations["accepted.transitgatewayID.gkop.io"]; connect {
			// check status to see if connection already established
			tgwAttachmentStatus, err := r.EC2Client.DescribeTransitGatewayAttachments(ctx, &ec2.DescribeTransitGatewayAttachmentsInput{
				TransitGatewayAttachmentIds: []string{transitgatewayID},
			})
			if err != nil {
				log.Info("Could not get TransitGateway Attachment")
				return err
			}
			// checks the first TransitGatewayAttachment returned and if it already exists, we do nothing
			if tgwAttachmentStatus.TransitGatewayAttachments[0].State == "available" {
				log.Info("TransitGateway already attached to VPC. No need to reconcile new Attachment.")
				return nil
			}
			// get the VPC ID using the "Name" tag defined in the first VPC CreateOrUpdate
			vpcOutput, err := r.EC2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
				Filters: []ec2types.Filter{{
					Name:   pointer("Name"),
					Values: []string{vpc.Name},
				}},
			})
			if err != nil {
				log.Info("Could not get VPC ID")
				return err
			}
			// should only have one matching VPC
			vpcId := vpcOutput.Vpcs[0].VpcId

			// get the transit Subnet IDs using tags
			transitSubnetOutput, err := r.EC2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
				Filters: []ec2types.Filter{{
					Name:   pointer("Type"),
					Values: []string{"transit"},
				}},
			})
			if err != nil {
				log.Info("Could not get the transit SubnetIDs")
				return err
			}
			subnetIds := make([]string, 0)
			for _, ids := range transitSubnetOutput.Subnets {
				subnetIds = append(subnetIds, *ids.SubnetId)
			}

			// since no attachment has been made already, try to attach using Resource IDs we just received
			tgwAttachmentOutput, err := r.EC2Client.CreateTransitGatewayVpcAttachment(ctx, &ec2.CreateTransitGatewayVpcAttachmentInput{
				SubnetIds:        subnetIds,
				TransitGatewayId: pointer(transitgatewayID),
				VpcId:            vpcId,
			})
			if err != nil {
				log.Info("Could not create TransitGateway Attachment")
				return err
			}
			// check status of newly created TGW Attachment
			tgwAttachmentStatus, err = r.EC2Client.DescribeTransitGatewayAttachments(ctx, &ec2.DescribeTransitGatewayAttachmentsInput{
				TransitGatewayAttachmentIds: []string{*tgwAttachmentOutput.TransitGatewayVpcAttachment.TransitGatewayAttachmentId},
			})
			// checks the newly created TransitGatewayAttachment
			if tgwAttachmentStatus.TransitGatewayAttachments[0].State != "available" {
				log.Info("New TransitGateway not available.")
				return nil
			}

			// get the Route Table ID
			// may need to make all these tags more specific to the NetworkGen
			routeTableOutput, err := r.EC2Client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
				Filters: []ec2types.Filter{{
					Name:   pointer("Type"),
					Values: []string{"private"},
				}},
			})
			if err != nil {
				return err
			}

			// create Routes inside each RouteTable
			for _, privateRouteTableIDs := range routeTableOutput.RouteTables {
				route1, err := r.EC2Client.CreateRoute(ctx, &ec2.CreateRouteInput{
					DestinationCidrBlock: pointer("11.8.8.0/29"),
					RouteTableId:         privateRouteTableIDs.RouteTableId,
					TransitGatewayId:     pointer(transitgatewayID),
				})
				if err != nil {
					return err
				}
				if !*route1.Return {
					log.Info("Route creation unsuccessful for route 1: 11.8.8.0/29")
				}
				route2, err := r.EC2Client.CreateRoute(ctx, &ec2.CreateRouteInput{
					DestinationCidrBlock: pointer("11.8.114.0/30"),
					RouteTableId:         privateRouteTableIDs.RouteTableId,
					TransitGatewayId:     pointer(transitgatewayID),
				})
				if err != nil {
					return err
				}
				if !*route2.Return {
					log.Info("Route creation unsuccessful for route 2: 11.8.114.0/30")
				}
				route3, err := r.EC2Client.CreateRoute(ctx, &ec2.CreateRouteInput{
					DestinationCidrBlock: pointer("11.8.114.6/32"),
					RouteTableId:         privateRouteTableIDs.RouteTableId,
					TransitGatewayId:     pointer(transitgatewayID),
				})
				if err != nil {
					return err
				}
				if !*route3.Return {
					log.Info("Route creation unsuccessful for route 3: 11.8.114.6/32")
				}
				route4, err := r.EC2Client.CreateRoute(ctx, &ec2.CreateRouteInput{
					DestinationCidrBlock: pointer("11.8.114.9/32"),
					RouteTableId:         privateRouteTableIDs.RouteTableId,
					TransitGatewayId:     pointer(transitgatewayID),
				})
				if err != nil {
					return err
				}
				if !*route4.Return {
					log.Info("Route creation unsuccessful for route 4: 11.8.114.9/32")
				}
				route5, err := r.EC2Client.CreateRoute(ctx, &ec2.CreateRouteInput{
					DestinationCidrBlock: pointer("100.84.72.0/24"),
					RouteTableId:         privateRouteTableIDs.RouteTableId,
					TransitGatewayId:     pointer(transitgatewayID),
				})
				if err != nil {
					return err
				}
				if !*route5.Return {
					log.Info("Route creation unsuccessful for route 5: 100.84.72.0/24")
				}
				route6, err := r.EC2Client.CreateRoute(ctx, &ec2.CreateRouteInput{
					DestinationCidrBlock: pointer("100.84.80.0/24"),
					RouteTableId:         privateRouteTableIDs.RouteTableId,
					TransitGatewayId:     pointer(transitgatewayID),
				})
				if err != nil {
					return err
				}
				if !*route6.Return {
					log.Info("Route creation unsuccessful for route 6: 100.84.80.0/24")
				}
				route7, err := r.EC2Client.CreateRoute(ctx, &ec2.CreateRouteInput{
					DestinationCidrBlock: pointer("100.84.88.0/24"),
					RouteTableId:         privateRouteTableIDs.RouteTableId,
					TransitGatewayId:     pointer(transitgatewayID),
				})
				if err != nil {
					return err
				}
				if !*route7.Return {
					log.Info("Route creation unsuccessful for route 7: 100.84.88.0/24")
				}
				route8, err := r.EC2Client.CreateRoute(ctx, &ec2.CreateRouteInput{
					DestinationCidrBlock: pointer("208.74.164.0/26"),
					RouteTableId:         privateRouteTableIDs.RouteTableId,
					TransitGatewayId:     pointer(transitgatewayID),
				})
				if err != nil {
					return err
				}
				if !*route8.Return {
					log.Info("Route creation unsuccessful for route 8: 208.74.164.0/26")
				}
				route9, err := r.EC2Client.CreateRoute(ctx, &ec2.CreateRouteInput{
					DestinationCidrBlock: pointer("10.0.0.0/8"),
					RouteTableId:         privateRouteTableIDs.RouteTableId,
					TransitGatewayId:     pointer(transitgatewayID),
				})
				if err != nil {
					return err
				}
				if !*route9.Return {
					log.Info("Route creation unsuccessful for route 9: 10.0.0.0/8")
				}
			}

		}

		printIfDiff(log, orig, vpc)
		// Set controller to own VPC
		err = ctrl.SetControllerReference(ng, vpc, r.Scheme)
		return err
	})

	return ctrl.Result{}, nil
}

func (r *NetworkGeneratorReconciler) vpcForNetworkGen(ng *kosmosv1.NetworkGenerator) *v1alpha1.VPC {

	vpcName := ng.Spec.Environment
	tags := ng.Spec.Tags
	var nameTag v1alpha1.Tag
	nameTag.Key = pointer("Name")
	nameTag.Value = pointer(vpcName)
	tags = append(tags, &nameTag)
	return &v1alpha1.VPC{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vpcName,
			Namespace: ng.Namespace,
		},
		Spec: v1alpha1.VPCSpec{
			CIDRBlocks:         ng.Spec.CIDRBlocks,
			EnableDNSSupport:   pointer(true),
			EnableDNSHostnames: pointer(true),
			Tags:               tags,
		},
	}
}
func (r *NetworkGeneratorReconciler) subnetForNetworkGen(ng *kosmosv1.NetworkGenerator, vpc *v1alpha1.VPC, subnet *v1alpha1.Subnet, rt string, cidr string, region string, zone string, subnetType string) *v1alpha1.Subnet {

	subnetName := subnet.Name
	tags := ng.Spec.Tags
	var nameTag v1alpha1.Tag
	var typeTag v1alpha1.Tag
	nameTag.Key = pointer("Name")
	nameTag.Value = pointer(subnetName)
	tags = append(tags, &nameTag)
	typeTag.Key = pointer("Type")
	typeTag.Value = pointer(subnetType)
	tags = append(tags, &typeTag)

	return &v1alpha1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      subnetName,
			Namespace: ng.Namespace,
		},
		Spec: v1alpha1.SubnetSpec{
			AvailabilityZone: pointer(region + zone),
			CIDRBlock:        &cidr,
			VPCRef: &ackv1alpha1.AWSResourceReferenceWrapper{
				From: &ackv1alpha1.AWSResourceReference{
					Name: &ng.Spec.Environment,
				},
			},
			RouteTableRefs: []*ackv1alpha1.AWSResourceReferenceWrapper{
				{
					From: &ackv1alpha1.AWSResourceReference{
						Name: &rt,
					},
				},
			},
			Tags: tags,
		},
	}
}
func (r *NetworkGeneratorReconciler) igForNetworkGen(ng *kosmosv1.NetworkGenerator, vpc *v1alpha1.VPC) *v1alpha1.InternetGateway {

	igName := ng.Spec.Environment + "-vpc"
	tags := ng.Spec.Tags
	var nameTag v1alpha1.Tag
	nameTag.Key = pointer("Name")
	nameTag.Value = pointer(igName)
	tags = append(tags, &nameTag)

	return &v1alpha1.InternetGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      igName,
			Namespace: ng.Namespace,
		},
		Spec: v1alpha1.InternetGatewaySpec{
			VPCRef: &ackv1alpha1.AWSResourceReferenceWrapper{
				From: &ackv1alpha1.AWSResourceReference{
					Name: &vpc.Name,
				},
			},
			Tags: tags,
		},
	}
}
func (r *NetworkGeneratorReconciler) privateRouteTableForNetworkGen(ng *kosmosv1.NetworkGenerator, vpc *v1alpha1.VPC, name string, region string, zone string) *v1alpha1.RouteTable {
	tags := ng.Spec.Tags
	var nameTag v1alpha1.Tag
	var typeTag v1alpha1.Tag
	nameTag.Key = pointer("Name")
	nameTag.Value = pointer(name)
	tags = append(tags, &nameTag)
	typeTag.Key = pointer("Type")
	typeTag.Value = pointer("private")
	tags = append(tags, &typeTag)
	natGatewayName := ng.Spec.Environment + "-vpc-" + region + zone
	return &v1alpha1.RouteTable{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ng.Namespace,
		},
		Spec: v1alpha1.RouteTableSpec{
			VPCRef: &ackv1alpha1.AWSResourceReferenceWrapper{
				From: &ackv1alpha1.AWSResourceReference{
					Name: &vpc.Name,
				},
			},
			Routes: []*v1alpha1.CreateRouteInput{
				{
					GatewayRef: &ackv1alpha1.AWSResourceReferenceWrapper{
						From: &ackv1alpha1.AWSResourceReference{
							Name: &natGatewayName,
						},
					},
					DestinationCIDRBlock: pointer("0.0.0.0/0"),
				},
			},
			Tags: tags,
		},
	}
}
func (r *NetworkGeneratorReconciler) publicRouteTableForNetworkGen(ng *kosmosv1.NetworkGenerator, vpc *v1alpha1.VPC, name string, region string, zone string) *v1alpha1.RouteTable {
	tags := ng.Spec.Tags
	var nameTag v1alpha1.Tag
	nameTag.Key = pointer("Name")
	nameTag.Value = pointer(name)
	tags = append(tags, &nameTag)
	igwName := ng.Spec.Environment + "-vpc"
	return &v1alpha1.RouteTable{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ng.Namespace,
		},
		Spec: v1alpha1.RouteTableSpec{
			VPCRef: &ackv1alpha1.AWSResourceReferenceWrapper{
				From: &ackv1alpha1.AWSResourceReference{
					Name: &vpc.Name,
				},
			},
			Routes: []*v1alpha1.CreateRouteInput{
				{
					GatewayRef: &ackv1alpha1.AWSResourceReferenceWrapper{
						From: &ackv1alpha1.AWSResourceReference{
							Name: &igwName,
						},
					},
					DestinationCIDRBlock: pointer("0.0.0.0/0"),
				},
			},
			Tags: tags,
		},
	}
}

func (r *NetworkGeneratorReconciler) emptyRouteTableForNetworkGen(ng *kosmosv1.NetworkGenerator, vpc *v1alpha1.VPC, name string, region string, zone string) *v1alpha1.RouteTable {
	tags := ng.Spec.Tags
	var nameTag v1alpha1.Tag
	nameTag.Key = pointer("Name")
	nameTag.Value = pointer(name)
	tags = append(tags, &nameTag)
	return &v1alpha1.RouteTable{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ng.Namespace,
		},
		Spec: v1alpha1.RouteTableSpec{
			VPCRef: &ackv1alpha1.AWSResourceReferenceWrapper{
				From: &ackv1alpha1.AWSResourceReference{
					Name: &vpc.Name,
				},
			},
			Tags: tags,
		},
	}
}

func (r *NetworkGeneratorReconciler) elasticIPForNetworkGen(ng *kosmosv1.NetworkGenerator, name string) *v1alpha1.ElasticIPAddress {
	tags := ng.Spec.Tags
	var nameTag v1alpha1.Tag
	nameTag.Key = pointer("Name")
	nameTag.Value = pointer(name)
	tags = append(tags, &nameTag)
	return &v1alpha1.ElasticIPAddress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ng.Namespace,
		},
		Spec: v1alpha1.ElasticIPAddressSpec{
			Tags: tags,
		},
	}
}
func (r *NetworkGeneratorReconciler) natGatewayForNetworkGen(ng *kosmosv1.NetworkGenerator, subnetRefName string, name string) *v1alpha1.NATGateway {
	tags := ng.Spec.Tags
	var nameTag v1alpha1.Tag
	nameTag.Key = pointer("Name")
	nameTag.Value = pointer(name)
	tags = append(tags, &nameTag)
	return &v1alpha1.NATGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ng.Namespace,
		},
		Spec: v1alpha1.NATGatewaySpec{
			AllocationRef: &ackv1alpha1.AWSResourceReferenceWrapper{
				From: &ackv1alpha1.AWSResourceReference{
					Name: &name,
				},
			},
			SubnetRef: &ackv1alpha1.AWSResourceReferenceWrapper{
				From: &ackv1alpha1.AWSResourceReference{
					Name: &subnetRefName,
				},
			},
			ConnectivityType: pointer("public"),
			Tags:             tags,
		},
	}
}

func (r *NetworkGeneratorReconciler) vpcEndpointforNetworkGen(ng *kosmosv1.NetworkGenerator, vpc *v1alpha1.VPC, service string, name string) *v1alpha1.VPCEndpoint {
	tags := ng.Spec.Tags
	var nameTag v1alpha1.Tag
	nameTag.Key = pointer("Name")
	nameTag.Value = pointer(name)
	tags = append(tags, &nameTag)
	serviceName := "com.amazonaws." + ng.Spec.Region + "." + service
	privateRouteTableRef := ng.Spec.Environment + "vpc-private-" + ng.Spec.Region
	serviceSubnetRef := ng.Spec.Environment + "vpc-service-" + ng.Spec.Region
	publicRouteTableRef := ng.Spec.Environment + "vpc-public"
	// Endpoint Type changes based on service
	if ng.Spec.SubnetDesign == "gkop-new" {
		if (service == "kms") || (service == "sqs") || (service == "secretsmanager") {
			return &v1alpha1.VPCEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ng.Namespace,
				},
				Spec: v1alpha1.VPCEndpointSpec{
					VPCEndpointType:   pointer("Interface"),
					ServiceName:       &serviceName,
					PrivateDNSEnabled: pointer(false),
					RouteTableRefs: []*ackv1alpha1.AWSResourceReferenceWrapper{
						{
							From: &ackv1alpha1.AWSResourceReference{
								Name: pointer(privateRouteTableRef + "a"),
							},
						},
						{
							From: &ackv1alpha1.AWSResourceReference{
								Name: pointer(privateRouteTableRef + "b"),
							},
						},
						{
							From: &ackv1alpha1.AWSResourceReference{
								Name: pointer(privateRouteTableRef + "c"),
							},
						},
					},
					Tags: tags,
				},
			}
		} else {
			return &v1alpha1.VPCEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ng.Namespace,
				},
				Spec: v1alpha1.VPCEndpointSpec{
					VPCEndpointType:   pointer("Gateway"),
					ServiceName:       &serviceName,
					PrivateDNSEnabled: pointer(false),
					RouteTableRefs: []*ackv1alpha1.AWSResourceReferenceWrapper{
						{
							From: &ackv1alpha1.AWSResourceReference{
								Name: pointer(privateRouteTableRef + "a"),
							},
						},
						{
							From: &ackv1alpha1.AWSResourceReference{
								Name: pointer(privateRouteTableRef + "b"),
							},
						},
						{
							From: &ackv1alpha1.AWSResourceReference{
								Name: pointer(privateRouteTableRef + "c"),
							},
						},
						{
							From: &ackv1alpha1.AWSResourceReference{
								Name: pointer(publicRouteTableRef),
							},
						},
					},
					Tags: tags,
				},
			}
		}
	} else {
		// Switch to service subnets for GKOP legacy design
		// Endpoint Type changes based on service
		if (service == "kms") || (service == "sqs") || (service == "secretsmanager") {
			return &v1alpha1.VPCEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ng.Namespace,
				},
				Spec: v1alpha1.VPCEndpointSpec{
					VPCEndpointType:   pointer("Interface"),
					ServiceName:       &serviceName,
					PrivateDNSEnabled: pointer(false),
					SubnetRefs: []*ackv1alpha1.AWSResourceReferenceWrapper{
						{
							From: &ackv1alpha1.AWSResourceReference{
								Name: pointer(serviceSubnetRef + "a"),
							},
						},
						{
							From: &ackv1alpha1.AWSResourceReference{
								Name: pointer(serviceSubnetRef + "b"),
							},
						},
						{
							From: &ackv1alpha1.AWSResourceReference{
								Name: pointer(serviceSubnetRef + "c"),
							},
						},
					},
					Tags: tags,
				},
			}
		} else {
			return &v1alpha1.VPCEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: ng.Namespace,
				},
				Spec: v1alpha1.VPCEndpointSpec{
					VPCEndpointType:   pointer("Gateway"),
					ServiceName:       &serviceName,
					PrivateDNSEnabled: pointer(false),
					RouteTableRefs: []*ackv1alpha1.AWSResourceReferenceWrapper{
						{
							From: &ackv1alpha1.AWSResourceReference{
								Name: pointer(privateRouteTableRef + "a"),
							},
						},
						{
							From: &ackv1alpha1.AWSResourceReference{
								Name: pointer(privateRouteTableRef + "b"),
							},
						},
						{
							From: &ackv1alpha1.AWSResourceReference{
								Name: pointer(privateRouteTableRef + "c"),
							},
						},
						{
							From: &ackv1alpha1.AWSResourceReference{
								Name: pointer(publicRouteTableRef),
							},
						},
					},
					Tags: tags,
				},
			}
		}
	}

}

func (r *NetworkGeneratorReconciler) adoptedResourceForNetworkGen(ar *kosmosv1.ExistingAWSResources, ng *kosmosv1.NetworkGenerator, kind string) *ackv1alpha1.AdoptedResource {
	return &ackv1alpha1.AdoptedResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ar.Name,
			Namespace: ng.Namespace,
		},
		Spec: ackv1alpha1.AdoptedResourceSpec{
			AWS: &ackv1alpha1.AWSIdentifiers{
				NameOrID: ar.ResourceID,
			},
			Kubernetes: &ackv1alpha1.ResourceWithMetadata{
				GroupKind: metav1.GroupKind{Group: "ec2.services.k8s.aws", Kind: kind},
				Metadata: &ackv1alpha1.PartialObjectMeta{
					Name:      "adopted" + ar.Name,
					Namespace: ng.Namespace,
				},
			},
		},
	}
}

func (r *NetworkGeneratorReconciler) routeTableForNetworkGenPatch(ng *kosmosv1.NetworkGenerator, vpc *v1alpha1.VPC, tgw *v1alpha1.TransitGateway, name string, region string, zone string) *v1alpha1.RouteTable {

	return &v1alpha1.RouteTable{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ng.Namespace,
		},
		Spec: v1alpha1.RouteTableSpec{
			VPCRef: &ackv1alpha1.AWSResourceReferenceWrapper{
				From: &ackv1alpha1.AWSResourceReference{
					Name: &vpc.Name,
				},
			},
			Routes: []*v1alpha1.CreateRouteInput{
				{
					TransitGatewayRef: &ackv1alpha1.AWSResourceReferenceWrapper{
						From: &ackv1alpha1.AWSResourceReference{
							Name: &tgw.Name,
						},
					},
					DestinationCIDRBlock: pointer("10.0.0.0/8"),
				},
				{
					TransitGatewayRef: &ackv1alpha1.AWSResourceReferenceWrapper{
						From: &ackv1alpha1.AWSResourceReference{
							Name: &tgw.Name,
						},
					},
					DestinationCIDRBlock: pointer("11.8.8.0/29"),
				},
				{
					TransitGatewayRef: &ackv1alpha1.AWSResourceReferenceWrapper{
						From: &ackv1alpha1.AWSResourceReference{
							Name: &tgw.Name,
						},
					},
					DestinationCIDRBlock: pointer("11.8.114.0/30"),
				},
				{
					TransitGatewayRef: &ackv1alpha1.AWSResourceReferenceWrapper{
						From: &ackv1alpha1.AWSResourceReference{
							Name: &tgw.Name,
						},
					},
					DestinationCIDRBlock: pointer("11.8.114.6/32"),
				},
				{
					TransitGatewayRef: &ackv1alpha1.AWSResourceReferenceWrapper{
						From: &ackv1alpha1.AWSResourceReference{
							Name: &tgw.Name,
						},
					},
					DestinationCIDRBlock: pointer("11.8.144.0/32"),
				},
				{
					TransitGatewayRef: &ackv1alpha1.AWSResourceReferenceWrapper{
						From: &ackv1alpha1.AWSResourceReference{
							Name: &tgw.Name,
						},
					},
					DestinationCIDRBlock: pointer("100.84.72.0/24"),
				},
				{
					TransitGatewayRef: &ackv1alpha1.AWSResourceReferenceWrapper{
						From: &ackv1alpha1.AWSResourceReference{
							Name: &tgw.Name,
						},
					},
					DestinationCIDRBlock: pointer("100.84.80.0/24"),
				},
				{
					TransitGatewayRef: &ackv1alpha1.AWSResourceReferenceWrapper{
						From: &ackv1alpha1.AWSResourceReference{
							Name: &tgw.Name,
						},
					},
					DestinationCIDRBlock: pointer("100.84.88.0/24"),
				},
				{
					TransitGatewayRef: &ackv1alpha1.AWSResourceReferenceWrapper{
						From: &ackv1alpha1.AWSResourceReference{
							Name: &tgw.Name,
						},
					},
					DestinationCIDRBlock: pointer("208.74.164.0/26"),
				},
			},
		},
	}
}

// a is the existing object, b is the expected
func printIfDiff(log logr.Logger, a metav1.Object, b metav1.Object) {
	// If object has not been created yet, do not calculate diff
	if !a.GetCreationTimestamp().Time.IsZero() {
		diff := pretty.Diff(a, b)
		if len(diff) > 0 {
			log.Info(fmt.Sprintf("Difference(s) in %s found:\n%s", a.GetSelfLink(), strings.Join(diff, "\n")))
		}
	}
}

func pointer[t any](v t) *t {
	return &v
}

// SetupWithManager sets up the controller with the Manager.
func (r *NetworkGeneratorReconciler) SetupWithManager(mgr ctrl.Manager) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&kosmosv1.NetworkGenerator{}).
		Owns(&ackv1alpha1.AdoptedResource{}).
		Owns(&v1alpha1.VPC{}).
		Owns(&v1alpha1.InternetGateway{}).
		Owns(&v1alpha1.RouteTable{}).
		Owns(&v1alpha1.Subnet{}).
		Owns(&v1alpha1.ElasticIPAddress{}).
		Owns(&v1alpha1.NATGateway{}).
		Owns(&v1alpha1.VPCEndpoint{}).
		Complete(r)
}
