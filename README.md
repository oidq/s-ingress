# S-Ingress

S-Ingress is a simple Kubernetes Ingress Controller implementation written in pure Go.
It was created from scratch to serve as a replacement of
[ingress-nginx](https://github.com/kubernetes/ingress-nginx).

> The initial version of this controller was created for use in a specific cluster,
> so it is not necessarily a full implementation of Ingress API,
> nor full ingress-nginx replacement.

> [!WARNING]
> 🚧 There is at least one cluster running this to serve traffic,
> but the controller is still under heavy development and might be unstable. 🚧

## Features

The S-Ingress supports basics of [Ingress API](https://kubernetes.io/docs/concepts/services-networking/ingress/),
however, there are some missing features, see [TODO](#todo). On the other hand, it supports TLS and HTTP/1.1, HTTP/2
and HTTP/3 (via [quic-go](https://quic-go.net/)) and features some more advanced annotations, see
[Annotations](#ingress-annotations).

Additionally, it is capable of TCP proxying with [PROXY](https://www.haproxy.org/download/1.8/doc/proxy-protocol.txt)
protocol. Websockets are also supported by default.

The S-Ingress has basic support for the Prometheus metrics.

### TODO

- Ingress API
  - support `defaultBackend`
  - support wildcard host matching
  - prevent updates from multiple controllers replica
  - support `ImplementationSpecific` path type
- Improve Helm chart
- Better logging
- Improve documentation

## Deployment

S-Ingress can be deployed with provided [helm chart](charts/s-ingress). See provided
[values.yaml](charts/s-ingress/values.yaml) for further configuration.

The snippet below provides a very simple way to deploy S-Ingress and test it with a dummy service.

```shell
# Install s-ingress
helm install --dry-run s-ingress oci://codeberg.org/oidq/charts/s-ingress 

# Install example Pod + Ingress
kubectl apply -f doc/example_ingress.yaml 
```

## Configuration

### Global configuration

Name of the global configuration ConfigMap is passed to S-Ingress with env `CONTROLLER_CONFIGMAP`.
The ConfigMap must contain key `config.yaml` with the configuration. The controller will listen for changes
and implement some subsections of the rules immediately, however, more drastic changes will not be reflected
until Pod restarts.

See [config.yaml](./pkg/config/config.yaml) for commented YAML configuration.

### Ingress Annotations

Some aspects of the proxying can be manipulated per Ingress object with Kubernetes annotations.

#### Auth

##### IP Authorization

```yaml
s-ingress.oidq.dev/allow-ip: "10.0.0.0/24,2a01::/64"
```

##### Basic Authentication

```yaml
s-ingress.oidq.dev/basic-auth-realm: "Super Secret Site"
s-ingress.oidq.dev/basic-auth-secret: "secret-in-current-namespace"
```

The supplied secret must be in the Ingress namespace and contain `auth` key with valid _htpasswd_ format.

##### Subrequest Authorization

```yaml
s-ingress.oidq.dev/auth-url: "http://auth:8443/authorize"
s-ingress.oidq.dev/auth-signin: "https://auth.my.domain/login"
```

S-Ingress will do a request to supplied `auth-url` with headers of the client request. If the response status
code is in the OK range (2xx), it allows the request, otherwise redirects to `auth-signin` (except for explicit
403 code, which returns 403 Forbidden from the proxy).

Value in `auth-signin` can contain placeholders `$host` and `$escaped_uri`, which can be used to pass redirect URL
(`...?rd=https%3A%2F%2F$host$escaped_uri`).

#### Security

##### Deny Route

```yaml
s-ingress.oidq.dev/deny-route: "^/admin"
```

This annotation can contain a regular expression which is matched against the request path and on match returns 403.
Beware that it is matched only on the path, not on the query or document part of URL.

## Development

The implementation is divided mainly into two areas. Package [./pkg/proxy](pkg/proxy) provides the
reverse proxy implementation used for proxying. Second package [./pkg/controller](pkg/controller) handles
the proxy configuration from K8s API and further reconciliation.

Furthermore, the directory [./modules](modules) contain _modules_, which provide some separate functionality,
mainly in the form of additional annotation.

### Tests

Tests are still mostly in progress. There are some tests, but they are not ideal in respect to their setup and
cases requiring full K8s control plane are not run in CI.