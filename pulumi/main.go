package main

import (
	"fmt"

	"github.com/pulumi/pulumi-kubernetes/sdk/v2/go/kubernetes/yaml"
	"github.com/pulumi/pulumi/sdk/v2/go/pulumi"

	certmanagerv1 "github.com/jakexks/cloudengsummit2020/pulumi/crds/certmanager/v1"
	appsv1 "github.com/pulumi/pulumi-kubernetes/sdk/v2/go/kubernetes/apps/v1"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v2/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v2/go/kubernetes/meta/v1"

	tls "github.com/pulumi/pulumi-tls/sdk/v2/go/tls"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Install cert-manager
		certManager, err := yaml.NewConfigFile(ctx, "cert-manager", &yaml.ConfigFileArgs{
			File: "../k8s/cert-manager.yaml",
		})
		if err != nil {
			return err
		}

		// Set up a Private CA and store it inside a Kubernetes Secret
		// to be consumed my cert-manager
		// Firstly the Private Key
		privateKey, err := tls.NewPrivateKey(ctx, "ca", &tls.PrivateKeyArgs{
			Algorithm: pulumi.String("RSA"),
			RsaBits:   pulumi.Int(2048),
		})
		if err != nil {
			return err
		}
		// Then a Self-Signed certificate
		caCert, err := tls.NewSelfSignedCert(ctx, "ca", &tls.SelfSignedCertArgs{
			KeyAlgorithm:        pulumi.String("RSA"),
			PrivateKeyPem:       privateKey.PrivateKeyPem,
			IsCaCertificate:     pulumi.Bool(true),
			ValidityPeriodHours: pulumi.Int(8760),
			AllowedUses: pulumi.StringArray{
				pulumi.String("key_encipherment"),
				pulumi.String("digital_signature"),
				pulumi.String("cert_signing"),
			},
			Subjects: &tls.SelfSignedCertSubjectArray{
				tls.SelfSignedCertSubjectArgs{
					CommonName:   pulumi.String("private-ca"),
					Organization: pulumi.String("Jetstack"),
				},
			},
		})
		if err != nil {
			return err
		}

		// Then a Secret which returns the CA
		_, err = corev1.NewSecret(ctx, "ca", &corev1.SecretArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String("ca"),
				Namespace: pulumi.String("default"),
			},
			Data: pulumi.StringMap{
				"tls.crt": caCert.CertPem.ApplyString(toBase64),
				"tls.key": privateKey.PrivateKeyPem.ApplyString(toBase64),
			},
			Type: pulumi.String("kubernetes.io/tls"),
		})
		if err != nil {
			return err
		}

		// Finally, create a cert-manager issuer that consumes this secret
		issuer, err := certmanagerv1.NewIssuer(ctx, "ca", &certmanagerv1.IssuerArgs{
			ApiVersion: pulumi.String("cert-manager.io/v1"),
			Kind:       pulumi.String("Issuer"),
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String("ca"),
				Namespace: pulumi.String("default"),
			},
			Spec: &certmanagerv1.IssuerSpecArgs{
				Ca: &certmanagerv1.IssuerSpecCaArgs{
					SecretName: pulumi.String("ca"),
				},
			},
		}, pulumi.DependsOn([]pulumi.Resource{certManager}))
		if err != nil {
			return err
		}

		// Now we need some certificates, the ping and the pong certs
		_, err = certmanagerv1.NewCertificate(ctx, "ping", &certmanagerv1.CertificateArgs{
			ApiVersion: pulumi.String("cert-manager.io/v1"),
			Kind:       pulumi.String("Certificate"),
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String("ping"),
				Namespace: pulumi.String("default"),
			},
			Spec: &certmanagerv1.CertificateSpecArgs{
				Duration:    pulumi.String("1h"),
				RenewBefore: pulumi.String("30m"),
				SecretName:  pulumi.String("ping-tls"),
				DnsNames: pulumi.StringArray{
					pulumi.String("ping.default.svc.cluster.local"),
				},
				IssuerRef: &certmanagerv1.CertificateSpecIssuerRefArgs{
					Kind: pulumi.String("Issuer"),
					Name: pulumi.String("ca"),
				},
				Usages: pulumi.StringArray{
					pulumi.String("key encipherment"),
					pulumi.String("digital signature"),
					pulumi.String("server auth"),
					pulumi.String("client auth"),
				},
			},
		}, pulumi.DependsOn([]pulumi.Resource{issuer}))
		if err != nil {
			return err
		}
		_, err = certmanagerv1.NewCertificate(ctx, "pong", &certmanagerv1.CertificateArgs{
			ApiVersion: pulumi.String("cert-manager.io/v1"),
			Kind:       pulumi.String("Certificate"),
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String("pong"),
				Namespace: pulumi.String("default"),
			},
			Spec: &certmanagerv1.CertificateSpecArgs{
				Duration:    pulumi.String("1h"),
				RenewBefore: pulumi.String("30m"),
				SecretName:  pulumi.String("pong-tls"),
				DnsNames: pulumi.StringArray{
					pulumi.String("pong.default.svc.cluster.local"),
				},
				IssuerRef: &certmanagerv1.CertificateSpecIssuerRefArgs{
					Kind: pulumi.String("Issuer"),
					Name: pulumi.String("ca"),
				},
				Usages: pulumi.StringArray{
					pulumi.String("key encipherment"),
					pulumi.String("digital signature"),
					pulumi.String("server auth"),
					pulumi.String("client auth"),
				},
			},
		}, pulumi.DependsOn([]pulumi.Resource{issuer}))
		if err != nil {
			return err
		}

		// Let's deploy the two mTLS apps
		pingLabels := pulumi.StringMap{
			"app": pulumi.String("ping"),
		}
		_, err = appsv1.NewDeployment(ctx, "ping", &appsv1.DeploymentArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String("ping"),
				Namespace: pulumi.String("default"),
				Labels:    pingLabels,
			},
			Spec: &appsv1.DeploymentSpecArgs{
				Replicas: pulumi.Int(1),
				Selector: &metav1.LabelSelectorArgs{
					MatchLabels: pingLabels,
				},
				Template: &corev1.PodTemplateSpecArgs{
					Metadata: &metav1.ObjectMetaArgs{
						Labels: pingLabels,
					},
					Spec: &corev1.PodSpecArgs{
						Volumes: corev1.VolumeArray{
							&corev1.VolumeArgs{
								Name: pulumi.String("tls"),
								Secret: &corev1.SecretVolumeSourceArgs{
									SecretName: pulumi.String("ping-tls"),
								},
							},
						},
						Containers: &corev1.ContainerArray{
							corev1.ContainerArgs{
								Image: pulumi.String("maartje/tls-ping-pong:2d1f1a81edf639ec2b1221f0cb7d84eb01bcae16"),
								Name:  pulumi.String("pingpong"),
								Command: pulumi.StringArray{
									pulumi.String("pingpong"),
									pulumi.String("-endpoint=https://pong.default.svc.cluster.local:8443/ping"),
									pulumi.String("-ca-file=/etc/ssl/private/ca.crt"),
									pulumi.String("-cert-file=/etc/ssl/private/tls.crt"),
									pulumi.String("-key-file=/etc/ssl/private/tls.key"),
								},
								Ports: corev1.ContainerPortArray{
									&corev1.ContainerPortArgs{
										ContainerPort: pulumi.Int(8443),
										Name:          pulumi.String("internal-https"),
									},
									&corev1.ContainerPortArgs{
										ContainerPort: pulumi.Int(9443),
										Name:          pulumi.String("external-https"),
									},
								},
								VolumeMounts: corev1.VolumeMountArray{
									&corev1.VolumeMountArgs{
										MountPath: pulumi.String("/etc/ssl/private"),
										Name:      pulumi.String("tls"),
										ReadOnly:  pulumi.Bool(true),
									},
								},
							},
						},
					},
				},
			},
		})
		if err != nil {
			return err
		}
		pongLabels := pulumi.StringMap{
			"app": pulumi.String("pong"),
		}
		_, err = appsv1.NewDeployment(ctx, "pong", &appsv1.DeploymentArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String("pong"),
				Namespace: pulumi.String("default"),
				Labels:    pongLabels,
			},
			Spec: &appsv1.DeploymentSpecArgs{
				Replicas: pulumi.Int(1),
				Selector: &metav1.LabelSelectorArgs{
					MatchLabels: pongLabels,
				},
				Template: &corev1.PodTemplateSpecArgs{
					Metadata: &metav1.ObjectMetaArgs{
						Labels: pongLabels,
					},
					Spec: &corev1.PodSpecArgs{
						Volumes: corev1.VolumeArray{
							&corev1.VolumeArgs{
								Name: pulumi.String("tls"),
								Secret: &corev1.SecretVolumeSourceArgs{
									SecretName: pulumi.String("pong-tls"),
								},
							},
						},
						Containers: &corev1.ContainerArray{
							corev1.ContainerArgs{
								Image: pulumi.String("maartje/tls-ping-pong:2d1f1a81edf639ec2b1221f0cb7d84eb01bcae16"),
								Name:  pulumi.String("pingpong"),
								Command: pulumi.StringArray{
									pulumi.String("pingpong"),
									pulumi.String("-endpoint=https://ping.default.svc.cluster.local:8443/ping"),
									pulumi.String("-ca-file=/etc/ssl/private/ca.crt"),
									pulumi.String("-cert-file=/etc/ssl/private/tls.crt"),
									pulumi.String("-key-file=/etc/ssl/private/tls.key"),
								},
								Ports: corev1.ContainerPortArray{
									&corev1.ContainerPortArgs{
										ContainerPort: pulumi.Int(8443),
										Name:          pulumi.String("internal-https"),
									},
									&corev1.ContainerPortArgs{
										ContainerPort: pulumi.Int(9443),
										Name:          pulumi.String("external-https"),
									},
								},
								VolumeMounts: corev1.VolumeMountArray{
									&corev1.VolumeMountArgs{
										MountPath: pulumi.String("/etc/ssl/private"),
										Name:      pulumi.String("tls"),
										ReadOnly:  pulumi.Bool(true),
									},
								},
							},
						},
					},
				},
			},
		})
		if err != nil {
			return err
		}

		// And services
		pingSvc, err := corev1.NewService(ctx, "ping", &corev1.ServiceArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String("ping"),
				Namespace: pulumi.String("default"),
				Labels:    pingLabels,
			},
			Spec: &corev1.ServiceSpecArgs{
				Type: pulumi.String("LoadBalancer"),
				Selector: pulumi.StringMap{
					"app": pulumi.String("ping"),
				},
				Ports: corev1.ServicePortArray{
					&corev1.ServicePortArgs{
						Name:       pulumi.String("internal-https"),
						Port:       pulumi.Int(8443),
						TargetPort: pulumi.String("internal-https"),
					},
					&corev1.ServicePortArgs{
						Name:       pulumi.String("external-https"),
						Port:       pulumi.Int(9443),
						TargetPort: pulumi.String("external-https"),
					},
				},
			},
		})
		if err != nil {
			return err
		}
		ctx.Export("pingURL", pingSvc.Status.ApplyString(func(val *corev1.ServiceStatus) string {
			return fmt.Sprintf("https://%s:9443", *val.LoadBalancer.Ingress[0].Ip)
		}))
		pongSvc, err := corev1.NewService(ctx, "pong", &corev1.ServiceArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String("pong"),
				Namespace: pulumi.String("default"),
				Labels:    pingLabels,
			},
			Spec: &corev1.ServiceSpecArgs{
				Type: pulumi.String("LoadBalancer"),
				Selector: pulumi.StringMap{
					"app": pulumi.String("pong"),
				},
				Ports: corev1.ServicePortArray{
					&corev1.ServicePortArgs{
						Name:       pulumi.String("internal-https"),
						Port:       pulumi.Int(8443),
						TargetPort: pulumi.String("internal-https"),
					},
					&corev1.ServicePortArgs{
						Name:       pulumi.String("external-https"),
						Port:       pulumi.Int(9443),
						TargetPort: pulumi.String("external-https"),
					},
				},
			},
		})
		if err != nil {
			return err
		}
		ctx.Export("pongURL", pongSvc.Status.ApplyString(func(val *corev1.ServiceStatus) string {
			return fmt.Sprintf("https://%s:9443", *val.LoadBalancer.Ingress[0].Ip)
		}))
		return nil
	})
}
