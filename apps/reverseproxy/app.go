package reverseproxy

import (
	"github.com/vertex-center/vertex/apps/reverseproxy/adapter"
	"github.com/vertex-center/vertex/apps/reverseproxy/core/port"
	"github.com/vertex-center/vertex/apps/reverseproxy/core/service"
	"github.com/vertex-center/vertex/apps/reverseproxy/handler"
	apptypes "github.com/vertex-center/vertex/core/types/app"
	"github.com/vertex-center/vertex/pkg/log"
	"github.com/vertex-center/vertex/pkg/router"
)

const (
	AppRoute = "/vx-reverse-proxy"
)

var (
	proxyFSAdapter port.ProxyAdapter

	proxyService port.ProxyService
)

type App struct {
	*apptypes.App
	proxy *ProxyRouter
}

func NewApp() *App {
	return &App{}
}

func (a *App) Initialize(app *apptypes.App) error {
	a.App = app

	proxyFSAdapter = adapter.NewProxyFSAdapter(nil)

	proxyService = service.NewProxyService(proxyFSAdapter)

	a.proxy = NewProxyRouter(proxyService)

	go func() {
		err := a.proxy.Start()
		if err != nil {
			log.Error(err)
		}
	}()

	app.Register(apptypes.Meta{
		ID:          "vx-reverse-proxy",
		Name:        "Vertex Reverse Proxy",
		Description: "Redirect traffic to your containers.",
		Icon:        "router",
	})

	app.RegisterRoutes(AppRoute, func(r *router.Group) {
		proxyHandler := handler.NewProxyHandler(proxyService)
		r.GET("/redirects", proxyHandler.GetRedirects)
		r.POST("/redirect", proxyHandler.AddRedirect)
		r.DELETE("/redirect/:id", proxyHandler.RemoveRedirect)
	})

	return nil
}

func (a *App) Uninitialize() error {
	return a.proxy.Stop()
}
