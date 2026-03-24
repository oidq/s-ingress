package oidc

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"time"

	"codeberg.org/oidq/s-ingress/pkg/config"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
	"k8s.io/apimachinery/pkg/types"
)

const callbackPath = "/_oidc/callback"

type module struct {
	config.Module

	clients map[string]*clientConfig

	callbackDomains map[string]*clientConfig
}

type clientConfig struct {
	name            string
	idTokenVerifier *oidc.IDTokenVerifier
	oauth2Config    *oauth2.Config

	callbackDomain string
	cookieDomain   string
	expiration     time.Duration

	encryptionKey []byte
	encryptor     jose.Encrypter
}

type ModuleConfig struct {
	Clients map[string]ClientConfig `yaml:"clients"`
}

type ClientConfig struct {
	// IssuerUrl is the url of the issuer, and it is expected that configuration will be found after
	// appending /.well-known/openid-configuration
	IssuerUrl string `yaml:"discoveryUrl"`

	// CallbackDomain is the domain to which IDP allows callback after authorization.
	// Route /_oidc/callback will be used.
	CallbackDomain string `yaml:"callbackDomain"`

	CookieDomain string `yaml:"cookieDomain"`

	// ClientSecretName is the name of a Secret object in the S-Ingress namespace, which must contain
	// at least three keys "CLIENT_ID", "CLIENT_SECRET" and "JWT_SECRET"
	ClientSecretName string `yaml:"clientSecretName"`

	// AuthExpiration is the expiration duration of the authorization info.
	//
	// If no value is supplied, the oidc module will follow oauth2 token expiration.
	AuthExpiration string `yaml:"authExpiration"`
}

func ModuleOidc(ctx context.Context, reconciler config.ModuleReconciler, config *config.ControllerConf) (config.ModuleInstance, error) {
	var conf ModuleConfig
	err := config.GetModuleConf("oidc", &conf)
	if err != nil {
		return nil, fmt.Errorf("error loading oidc configuration: %v", err)
	}

	clients := map[string]*clientConfig{}
	domains := map[string]*clientConfig{}
	for name, clientConf := range conf.Clients {
		c, err := getClientConfig(ctx, reconciler, name, clientConf)
		if err != nil {
			return nil, fmt.Errorf("error loading client %q configuration: %v", name, err)
		}

		clients[name] = c
		domains[c.callbackDomain] = c
	}

	return &module{
		clients:         clients,
		callbackDomains: domains,
	}, nil
}

func getClientConfig(ctx context.Context, reconciler config.ModuleReconciler, name string, conf ClientConfig) (*clientConfig, error) {
	secret, err := reconciler.GetSecret(ctx, types.NamespacedName{
		Namespace: reconciler.GetNamespace(),
		Name:      conf.ClientSecretName,
	})
	if err != nil {
		return nil, fmt.Errorf("error getting client secret %s/%s: %w", reconciler.GetNamespace(), conf.ClientSecretName, err)
	}
	clientId := string(secret.Data["CLIENT_ID"])
	clientSecret := string(secret.Data["CLIENT_SECRET"])
	jwtSecret := secret.Data["JWT_SECRET"]

	if len(jwtSecret) == 0 {
		return nil, fmt.Errorf("error getting client secret %s/%s: missing JWT_SECRET",
			reconciler.GetNamespace(), conf.ClientSecretName)
	}
	jwtNormalizedSecret := extractJwtAesKeySecret(jwtSecret)

	u := url.URL{Host: conf.CallbackDomain, Scheme: "https", Path: callbackPath}
	callbackUrl := u.String()

	provider, err := oidc.NewProvider(ctx, conf.IssuerUrl)
	if err != nil {
		log.Fatal(err)
	}
	oidcConfig := &oidc.Config{
		ClientID: clientId,
	}
	verifier := provider.Verifier(oidcConfig)

	oauth2Conf := &oauth2.Config{
		ClientID:     clientId,
		ClientSecret: clientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  callbackUrl,
		Scopes:       []string{oidc.ScopeOpenID, oidc.ScopeOfflineAccess, "profile", "email", "groups"},
	}

	enc, err := newEncrypter(jwtNormalizedSecret)
	if err != nil {
		return nil, fmt.Errorf("error creating encrypter: %w", err)
	}

	expiration, err := time.ParseDuration(defaultStr(conf.AuthExpiration, "0"))
	if err != nil {
		return nil, fmt.Errorf("error parsing auth expiration: %w", err)
	}

	return &clientConfig{
		name:            name,
		idTokenVerifier: verifier,
		expiration:      expiration,
		oauth2Config:    oauth2Conf,
		callbackDomain:  conf.CallbackDomain,
		cookieDomain:    conf.CookieDomain,
		encryptionKey:   jwtNormalizedSecret,
		encryptor:       enc,
	}, nil
}

func defaultStr(val, defaultVal string) string {
	if val == "" {
		return defaultVal
	}
	return val
}
