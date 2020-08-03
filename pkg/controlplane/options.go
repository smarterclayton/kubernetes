package controlplane

import (
	"errors"
	"fmt"

	"k8s.io/apiserver/pkg/server/egressselector"
	openapicommon "k8s.io/kube-openapi/pkg/common"
	"k8s.io/kubernetes/pkg/kubeapiserver/options"
	genericapiserver "k8s.io/apiserver/pkg/server"
)

// AuthenticationApplyTo requires already applied OpenAPIConfig and EgressSelector if present.
func AuthenticationApplyTo(o *options.BuiltInAuthenticationOptions, authInfo *genericapiserver.AuthenticationInfo, secureServing *genericapiserver.SecureServingInfo, egressSelector *egressselector.EgressSelector, openAPIConfig *openapicommon.Config) error {
	if o == nil {
		return nil
	}

	if openAPIConfig == nil {
		return errors.New("uninitialized OpenAPIConfig")
	}

	authenticatorConfig, err := o.ToAuthenticationConfig()
	if err != nil {
		return err
	}

	if authenticatorConfig.ClientCAContentProvider != nil {
		if err = authInfo.ApplyClientCert(authenticatorConfig.ClientCAContentProvider, secureServing); err != nil {
			return fmt.Errorf("unable to load client CA file: %v", err)
		}
	}
	if authenticatorConfig.RequestHeaderConfig != nil && authenticatorConfig.RequestHeaderConfig.CAContentProvider != nil {
		if err = authInfo.ApplyClientCert(authenticatorConfig.RequestHeaderConfig.CAContentProvider, secureServing); err != nil {
			return fmt.Errorf("unable to load client CA file: %v", err)
		}
	}

	// authInfo.SupportsBasicAuth = o.PasswordFile != nil && len(o.PasswordFile.BasicAuthFile) > 0

	authInfo.APIAudiences = o.APIAudiences
	// if o.ServiceAccounts != nil && o.ServiceAccounts.Issuer != "" && len(o.APIAudiences) == 0 {
	// 	authInfo.APIAudiences = authenticator.Audiences{o.ServiceAccounts.Issuer}
	// }

	// if o.ServiceAccounts.Lookup || utilfeature.DefaultFeatureGate.Enabled(features.TokenRequest) {
	// 	authenticatorConfig.ServiceAccountTokenGetter = serviceaccountcontroller.NewGetterFromClient(
	// 		extclient,
	// 		versionedInformer.Core().V1().Secrets().Lister(),
	// 		versionedInformer.Core().V1().ServiceAccounts().Lister(),
	// 		versionedInformer.Core().V1().Pods().Lister(),
	// 	)
	// }
	// authenticatorConfig.BootstrapTokenAuthenticator = bootstrap.NewTokenAuthenticator(
	// 	versionedInformer.Core().V1().Secrets().Lister().Secrets(metav1.NamespaceSystem),
	// )

	if egressSelector != nil {
		egressDialer, err := egressSelector.Lookup(egressselector.Master.AsNetworkContext())
		if err != nil {
			return err
		}
		authenticatorConfig.CustomDial = egressDialer
	}

	authInfo.Authenticator, openAPIConfig.SecurityDefinitions, err = authenticatorConfig.New()
	if err != nil {
		return err
	}

	return nil
}
