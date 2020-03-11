package cfg

import (
	"testing"

	// "github.com/vouch/vouch-proxy/pkg/structs"
	"github.com/stretchr/testify/assert"
)

func init() {
	// log.SetLevel(log.DebugLevel)
	InitForTestPurposes()
}

func TestConfigParsing(t *testing.T) {

	// UnmarshalKey(Branding.LCName, &cfg)
	log.Debugf("cfgPort %d", Cfg.Port)
	log.Debugf("cfgDomains %s", Cfg.Domains[0])

	assert.Equal(t, Cfg.Port, 9090)

	assert.NotEmpty(t, Cfg.JWT.MaxAge)

}

// Just test the merge
func TestConfigFileMerge(t *testing.T) {

	if err := os.Setenv(Branding.UCName+"_CONFIG", "../../config/test_config.yml"); err != nil {
		log.Error(err)
	}
	// log.Debug("opening config")
	setDevelopmentLogger()
	ParseConfig()
	SetDefaults()

	// set this 
	// cmdLineConfig = flag.String("config", "", "specify alternate .yml file as command line arg")

	// to the default one
	// assert the values

	// set up some test values

	// assert that they are overridden afterwards

}

// Test some config file and some env variable for precendence order
func TestConfigEnvVars(t *testing.T) {

	if err := os.Setenv(Branding.UCName+"_CONFIG", "../../config/test_config.yml"); err != nil {
		log.Error(err)
	}
	// log.Debug("opening config")
	setDevelopmentLogger()
	ParseConfig()
	SetDefaults()

	// set this 
	// cmdLineConfig = flag.String("config", "", "specify alternate .yml file as command line arg")

	// to the default one
	// assert the values

	// set up some test values

	// assert that they are overridden afterwards

}