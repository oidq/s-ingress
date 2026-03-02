package modules

import (
	"codeberg.org/oidq/s-ingress/modules/auth"
	"codeberg.org/oidq/s-ingress/modules/headers"
	"codeberg.org/oidq/s-ingress/modules/security"
	"codeberg.org/oidq/s-ingress/modules/websocket"
	"codeberg.org/oidq/s-ingress/pkg/config"
)

// Modules contain a list of all active modules and their order of activation.
var Modules = []config.ModuleCreator{
	security.ModuleEnforceHttps,

	auth.ModuleIpAuth,
	auth.ModuleBasicAuth,
	auth.ModuleSubrequestAuth,
	security.ModuleDenyRoute,
	websocket.ModuleWebsocket,

	headers.ModuleCustomHeader,
	headers.ModuleUpstreamHeader,
}
