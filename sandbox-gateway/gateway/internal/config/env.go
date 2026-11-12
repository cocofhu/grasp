package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Environment overrides belong either to the gateway or its selected backend.
// Empty values do not override YAML, which makes optional Compose values safe.
func applyEnv(c *Config) error {
	stringsByKey := map[string]*string{
		"SBGW_LISTEN":    &c.Server.Listen,
		"SBGW_DB_DRIVER": &c.Database.Driver, "SBGW_DB_PATH": &c.Database.Path,
		"SBGW_DB_DSN": &c.Database.DSN, "SBGW_DB_HOST": &c.Database.Host,
		"SBGW_DB_USER": &c.Database.User, "SBGW_DB_PASSWORD": &c.Database.Password,
		"SBGW_DB_NAME": &c.Database.Name,
	}
	intsByKey := map[string]*int{
		"SBGW_FINALIZE_TIMEOUT_SEC":   &c.Server.FinalizeTimeoutSeconds,
		"SBGW_DB_PORT":                &c.Database.Port,
		"SBGW_ORPHAN_GC_INTERVAL_SEC": &c.OrphanGC.IntervalSeconds,
		"SBGW_ORPHAN_GC_MIN_AGE_SEC":  &c.OrphanGC.MinAgeSeconds,
	}
	boolsByKey := map[string]*bool{"SBGW_ORPHAN_GC_ENABLED": &c.OrphanGC.Enabled}
	if d := c.Docker; d != nil {
		stringsByKey["SBGW_DOCKER_IMAGE"] = &d.Image
		stringsByKey["SBGW_DOCKER_BIND_IP"] = &d.BindIP
		stringsByKey["SBGW_DOCKER_NETWORK"] = &d.Network
	}
	if k := c.Kubernetes; k != nil {
		stringsByKey["SBGW_K8S_IMAGE"] = &k.Image
		stringsByKey["SBGW_K8S_KUBECONFIG"] = &k.Kubeconfig
		stringsByKey["SBGW_K8S_NAMESPACE"] = &k.Namespace
		stringsByKey["SBGW_K8S_STORAGE_CLASS"] = &k.StorageClass
		stringsByKey["SBGW_K8S_IMAGE_PULL_POLICY"] = &k.ImagePullPolicy
		boolsByKey["SBGW_K8S_IN_CLUSTER"] = &k.InCluster
		boolsByKey["SBGW_K8S_ENABLE_LB"] = &k.EnableLoadBalancer
	}
	if e := c.E2B; e != nil {
		stringsByKey["SBGW_E2B_API_KEY"] = &e.APIKey
		stringsByKey["SBGW_E2B_API_URL"] = &e.APIURL
		stringsByKey["SBGW_E2B_SANDBOX_URL"] = &e.SandboxURL
		stringsByKey["SBGW_E2B_OWNER_ID"] = &e.OwnerID
		stringsByKey["SBGW_E2B_STATE_DIR"] = &e.StateDir
		intsByKey["SBGW_E2B_BOOT_TIMEOUT_SEC"] = &e.BootTimeoutSeconds
		intsByKey["SBGW_E2B_LEASE_SEC"] = &e.LeaseSeconds
		intsByKey["SBGW_E2B_RENEW_INTERVAL_SEC"] = &e.RenewIntervalSeconds
		boolsByKey["SBGW_E2B_AUTO_PAUSE"] = &e.AutoPause
	}
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "SBGW_") {
			continue
		}
		switch key {
		case "SBGW_CONFIG", "SBGW_DRIVER", "SBGW_LOG_LEVEL", "SBGW_LOG_PRETTY":
			continue
		case "SBGW_API_KEYS":
			if value != "" {
				c.Auth.APIKeys = splitCSV(value)
			}
			continue
		}
		if destination, ok := stringsByKey[key]; ok {
			if value != "" {
				*destination = value
			}
			continue
		}
		if destination, ok := intsByKey[key]; ok {
			if value == "" {
				continue
			}
			number, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s must be an integer", key)
			}
			*destination = number
			continue
		}
		if destination, ok := boolsByKey[key]; ok {
			if value == "" {
				continue
			}
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("%s must be a boolean", key)
			}
			*destination = parsed
			continue
		}
		return fmt.Errorf("%s is not supported by the %s gateway configuration", key, c.Driver)
	}
	return nil
}

func splitCSV(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}
