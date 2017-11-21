package manager

import (
	"fmt"
	"os"
	"strings"

	"github.com/docker/infrakit/pkg/launch/inproc"
	logutil "github.com/docker/infrakit/pkg/log"
	"github.com/docker/infrakit/pkg/manager"
	"github.com/docker/infrakit/pkg/plugin"
	"github.com/docker/infrakit/pkg/rpc/mux"
	rpc "github.com/docker/infrakit/pkg/rpc/server"
	"github.com/docker/infrakit/pkg/run"
	"github.com/docker/infrakit/pkg/run/local"
	"github.com/docker/infrakit/pkg/run/scope"
	"github.com/docker/infrakit/pkg/template"
	"github.com/docker/infrakit/pkg/types"
)

const (
	// Kind is the canonical name of the plugin and also key used to locate the plugin in discovery
	Kind = "manager"

	// LookupName is the name used to look up the object via discovery
	LookupName = "group"

	// EnvOptionsBackend is the environment variable to use to set the default value of Options.Backend
	EnvOptionsBackend = "INFRAKIT_MANAGER_BACKEND"

	// EnvMuxListen is the listen string (:24864)
	EnvMuxListen = "INFRAKIT_MUX_LISTEN"

	// EnvAdvertise is the location of this node (127.0.0.1:24864)
	EnvAdvertise = "INFRAKIT_ADVERTISE"

	// EnvGroup is the group name backend
	EnvGroup = "INFRAKIT_MANAGER_GROUP"

	// EnvMetadata is the metadata backend
	EnvMetadata = "INFRAKIT_MANAGER_METADATA"

	// EnvMetadataUpdateInterval is the metadata backend update interval
	// if > 0, this will allow non-leader updates of metadata to be synced
	// to the leader. otherwise, a non-leader will not see the leader's updates
	// until it becomes the leader.  This is because even though the data is
	// persisted, it is not polled and read by the non-leaders on a regular basis
	// (unless this is set).
	EnvMetadataUpdateInterval = "INFRAKIT_MANAGER_METADATA_UPDATE_INTERVAL"

	// EnvLeaderCommitSpecsRetryInterval is the interval to wait between retries when
	// the manager becomes the leader and fails to commit the replicated specs.
	EnvLeaderCommitSpecsRetryInterval = "INFRAKIT_MANAGER_COMMIT_SPECS_RETRY_INTERVAL"
)

var (
	log                   = logutil.New("module", "run/manager")
	defaultOptionsBackend = local.Getenv(EnvOptionsBackend, "file")
)

func init() {
	inproc.Register(Kind, Run, DefaultOptions)
}

// Options capture the options for starting up the plugin.
type Options struct {
	manager.Options

	// Backend is the backend used for leadership, persistence, etc.
	// Possible values are file, etcd, and swarm
	Backend string

	// Settings is the configuration of the backend
	Settings *types.Any

	// Mux is the tcp frontend for remote connectivity
	Mux *MuxConfig

	cleanUpFunc func()
}

// MuxConfig is the struct for the mux frontend
type MuxConfig struct {
	// Listen string e.g. :24864
	Listen string

	// Advertise is the public listen string e.g. public_ip:24864
	Advertise string
}

// DefaultOptions return an Options with default values filled in.
var DefaultOptions = defaultOptions()

func defaultOptions() (options Options) {

	options = Options{
		Options: manager.Options{
			Group:                          plugin.Name(local.Getenv(EnvGroup, "group-stateless")),
			Metadata:                       plugin.Name(local.Getenv(EnvMetadata, "vars")),
			MetadataRefreshInterval:        types.MustParseDuration(local.Getenv(EnvMetadataUpdateInterval, "5s")),
			LeaderCommitSpecsRetries:       10,
			LeaderCommitSpecsRetryInterval: types.MustParseDuration(local.Getenv(EnvLeaderCommitSpecsRetryInterval, "2s")),
		},
		Mux: &MuxConfig{
			Listen:    local.Getenv(EnvMuxListen, ":24864"),
			Advertise: local.Getenv(EnvAdvertise, "localhost:24864"),
		},
	}

	options.Backend = os.Getenv(EnvOptionsBackend)
	switch options.Backend {
	case "swarm":
		options.Backend = "swarm"
		options.Settings = types.AnyValueMust(DefaultBackendSwarmOptions)
	case "etcd":
		options.Backend = "etcd"
		options.Settings = types.AnyValueMust(DefaultBackendEtcdOptions)
	case "file":
		options.Backend = "file"
		options.Settings = types.AnyValueMust(DefaultBackendFileOptions)
	default:
		options.Backend = "file"
		options.Settings = types.AnyValueMust(DefaultBackendFileOptions)
	}

	return
}

// Run runs the plugin, blocking the current thread.  Error is returned immediately
// if the plugin cannot be started.
func Run(scope scope.Scope, name plugin.Name,
	config *types.Any) (transport plugin.Transport, impls map[run.PluginCode]interface{}, onStop func(), err error) {

	options := defaultOptions()
	err = config.Decode(&options)
	if err != nil {
		return
	}

	log.Info("Decoded input", "config", options)
	log.Info("Starting up", "backend", options.Backend)

	options.Name = name
	options.Plugins = scope.Plugins

	switch strings.ToLower(options.Backend) {
	case "etcd":
		backendOptions := DefaultBackendEtcdOptions
		err = options.Settings.Decode(&backendOptions)
		if err != nil {
			return
		}
		log.Info("starting up etcd backend", "options", backendOptions)
		err = configEtcdBackends(backendOptions, &options)
		if err != nil {
			return
		}
		log.Info("etcd backend", "leader", options.Leader, "store", options.SpecStore, "cleanup", options.cleanUpFunc)
	case "file":
		backendOptions := DefaultBackendFileOptions
		err = options.Settings.Decode(&backendOptions)
		if err != nil {
			return
		}
		log.Info("starting up file backend", "options", backendOptions)
		err = configFileBackends(backendOptions, &options)
		if err != nil {
			return
		}
		log.Info("file backend", "leader", options.Leader, "store", options.SpecStore, "cleanup", options.cleanUpFunc)
	case "swarm":
		backendOptions := DefaultBackendSwarmOptions
		err = options.Settings.Decode(&backendOptions)
		if err != nil {
			return
		}
		log.Info("starting up swarm backend", "options", backendOptions)
		err = configSwarmBackends(backendOptions, &options)
		if err != nil {
			return
		}
		log.Info("swarm backend", "leader", options.Leader, "store", options.SpecStore, "cleanup", options.cleanUpFunc)
	default:
		err = fmt.Errorf("unknown backend:%v", options.Backend)
		return
	}

	mgr := manager.NewManager(options.Options)
	log.Info("Start manager", "m", mgr)

	_, err = mgr.Start()
	if err != nil {
		return
	}

	log.Info("Manager running")

	transport.Name = name

	impls = map[run.PluginCode]interface{}{
		run.Manager:           mgr,
		run.Controller:        mgr.Controllers,
		run.Group:             mgr.Groups,
		run.MetadataUpdatable: mgr.Metadata,
	}

	var muxServer rpc.Stoppable

	if options.Mux != nil {

		// Advertise value supports templating
		advertise := options.Mux.Advertise
		if t, err := scope.TemplateEngine(advertise, template.Options{MultiPass: false}); err == nil {
			// No error, assume that the value is a template URL
			if advertise, err = t.Render(nil); err != nil {
				fmt.Printf("Cannot start up mux server, advertise template '%v' rendering failed: %v\n", t, err)
				os.Exit(-1)
			}
		}

		log.Info("Starting mux server", "listen", options.Mux.Listen, "advertise", advertise)
		muxServer, err = mux.NewServer(options.Mux.Listen, advertise, options.Plugins,
			mux.Options{
				Leadership: options.Leader.Receive(),
				Registry:   options.LeaderStore,
			})
		if err != nil {
			fmt.Printf("Cannot start up mux server.  Error: %v\n", err)
			os.Exit(-1)
		}
	}

	onStop = func() {
		if options.cleanUpFunc != nil {
			options.cleanUpFunc()
		}
		if muxServer != nil {
			muxServer.Stop()
		}
	}

	log.Info("exported objects")
	return
}

type cleanup func()
