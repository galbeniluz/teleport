package automaticupgrades

import (
	"context"
	"net/url"

	"github.com/gravitational/trace"

	"github.com/gravitational/teleport/integrations/kube-agent-updater/pkg/maintenance"
	"github.com/gravitational/teleport/integrations/kube-agent-updater/pkg/version"
)

type Channels map[string]*Channel

func (c Channels) CheckAndSetDefaults() error {
	var errs []error
	var err error
	for name, channel := range c {
		err = trace.Wrap(channel.CheckAndSetDefaults(), "failed to create channel %s", name)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return trace.NewAggregate(errs...)
}

type Channel struct {
	ForwardURL      string `yaml:"forward_url,omitempty"`
	StaticVersion   string `yaml:"static_version,omitempty"`
	Critical        bool   `yaml:"critical"`
	versionGetter   version.Getter
	criticalTrigger maintenance.Trigger
}

func (c *Channel) CheckAndSetDefaults() error {
	switch {
	case c.ForwardURL != "" && (c.StaticVersion != "" || c.Critical):
		return trace.BadParameter("Cannot set both ForwardURL and (StaticVersion or Critical)")
	case c.ForwardURL != "":
		baseURL, err := url.Parse(c.ForwardURL)
		if err != nil {
			return trace.Wrap(err)
		}
		c.versionGetter = version.NewBasicHTTPVersionGetter(baseURL)
		c.criticalTrigger = maintenance.NewBasicHTTPMaintenanceTrigger("remote", baseURL)
	case c.StaticVersion != "":
		c.versionGetter = version.NewStaticGetter(c.StaticVersion, nil)
		c.criticalTrigger = maintenance.NewMaintenanceStaticTrigger("remote", c.Critical)
	default:
		return trace.BadParameter("Either ForwardURL or StaticVersion must be set")
	}
	return nil
}

func (c *Channel) GetVersion(ctx context.Context) (string, error) {
	return c.versionGetter.GetVersion(ctx)
}

func (c *Channel) GetCritical(ctx context.Context) (bool, error) {
	return c.criticalTrigger.CanStart(ctx, nil)
}
