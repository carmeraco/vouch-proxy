package cfg

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"

	"github.com/spf13/viper"
	securerandom "github.com/theckman/go-securerandom"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// config vouch jwt cookie configuration
type config struct {
	Logger        *zap.SugaredLogger
	FastLogger    *zap.Logger
	LogLevel      string   `mapstructure:"logLevel"`
	Listen        string   `mapstructure:"listen"`
	Port          int      `mapstructure:"port"`
	HealthCheck   bool     `mapstructure:"healthCheck"`
	Domains       []string `mapstructure:"domains"`
	WhiteList     []string `mapstructure:"whitelist"`
	AllowAllUsers bool     `mapstructure:"allowAllUsers"`
	PublicAccess  bool     `mapstructure:"publicAccess"`
	JWT           struct {
		MaxAge   int    `mapstructure:"maxAge"`
		Issuer   string `mapstructure:"issuer"`
		Secret   string `mapstructure:"secret"`
		Compress bool   `mapstructure:"compress"`
	}
	Cookie struct {
		Name     string `mapstructure:"name"`
		Domain   string `mapstructure:"domain"`
		Secure   bool   `mapstructure:"secure"`
		HTTPOnly bool   `mapstructure:"httpOnly"`
		MaxAge   int    `mapstructure:"maxage"`
	}

	Headers struct {
		JWT         string   `mapstructure:"jwt"`
		User        string   `mapstructure:"user"`
		QueryString string   `mapstructure:"querystring"`
		Redirect    string   `mapstructure:"redirect"`
		Success     string   `mapstructure:"success"`
		ClaimHeader string   `mapstructure:"claimheader"`
		Claims      []string `mapstructure:"claims"`
		AccessToken string   `mapstructure:"accesstoken"`
		IDToken     string   `mapstructure:"idtoken"`
	}
	DB struct {
		File string `mapstructure:"file"`
	}
	Session struct {
		Name string `mapstructure:"name"`
		Key  string `mapstructure:"key"`
	}
	TestURL  string   `mapstructure:"test_url"`
	TestURLs []string `mapstructure:"test_urls"`
	Testing  bool     `mapstructure:"testing"`
	WebApp   bool     `mapstructure:"webapp"`
}

// oauth config items endoint for access
type oauthConfig struct {
	Provider        string   `mapstructure:"provider"`
	ClientID        string   `mapstructure:"client_id"`
	ClientSecret    string   `mapstructure:"client_secret"`
	AuthURL         string   `mapstructure:"auth_url"`
	TokenURL        string   `mapstructure:"token_url"`
	RedirectURL     string   `mapstructure:"callback_url"`
	RedirectURLs    []string `mapstructure:"callback_urls"`
	Scopes          []string `mapstructure:"scopes"`
	UserInfoURL     string   `mapstructure:"user_info_url"`
	PreferredDomain string   `mapstructre:"preferredDomain"`
}

// OAuthProviders holds the stings for
type OAuthProviders struct {
	Google        string
	GitHub        string
	IndieAuth     string
	ADFS          string
	OIDC          string
	HomeAssistant string
	OpenStax      string
}

type branding struct {
	LCName    string // lower case
	UCName    string // upper case
	CcName    string // camel case
	OldLCName string // lasso
	URL       string // https://github.com/vouch/vouch-proxy
}

var (
	// Branding that's our name
	Branding = branding{"vouch", "VOUCH", "Vouch", "lasso", "https://github.com/vouch/vouch-proxy"}

	// Cfg the main exported config variable
	Cfg config

	// GenOAuth exported OAuth config variable
	// TODO: I think GenOAuth and OAuthConfig can be combined!
	// perhaps by https://golang.org/doc/effective_go.html#embedding
	GenOAuth *oauthConfig

	// OAuthClient is the configured client which will call the provider
	// this actually carries the oauth2 client ala oauthclient.Client(oauth2.NoContext, providerToken)
	OAuthClient *oauth2.Config
	// OAuthopts authentication options
	OAuthopts oauth2.AuthCodeOption

	// Providers static strings to test against
	Providers = &OAuthProviders{
		Google:        "google",
		GitHub:        "github",
		IndieAuth:     "indieauth",
		ADFS:          "adfs",
		OIDC:          "oidc",
		HomeAssistant: "homeassistant",
		OpenStax:      "openstax",
	}

	// RequiredOptions must have these fields set for minimum viable config
	RequiredOptions = []string{"oauth.provider", "oauth.client_id"}

	// RootDir is where Vouch Proxy looks for ./config/config.yml, ./data, ./static and ./templates
	RootDir string

	secretFile    string
	cmdLineConfig *string
	logger        *zap.Logger
	log           *zap.SugaredLogger
	atom          zap.AtomicLevel
)

const (
	// for a Base64 string we need 44 characters to get 32bytes (6 bits per char)
	minBase64Length = 44
	base64Bytes     = 32
)

func init() {

	atom = zap.NewAtomicLevel()
	encoderCfg := zap.NewProductionEncoderConfig()

	logger = zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.Lock(os.Stdout),
		atom,
	))

	defer logger.Sync() // flushes buffer, if any
	log = logger.Sugar()
	Cfg.FastLogger = logger
	Cfg.Logger = log

	// Handle -healthcheck argument
	healthCheck := flag.Bool("healthcheck", false, "invoke healthcheck (check process return value)")
	// can pass loglevel on the command line
	ll := flag.String("loglevel", "", "enable debug log output")
	// from config file
	port := flag.Int("port", -1, "port")
	help := flag.Bool("help", false, "show usage")
	cmdLineConfig = flag.String("config", "", "specify alternate .yml file as command line arg")
	flag.Parse()

	// set RootDir from VOUCH_ROOT env var, or to the executable's directory
	if os.Getenv(Branding.UCName+"_ROOT") != "" {
		RootDir = os.Getenv(Branding.UCName + "_ROOT")
		log.Infof("set cfg.RootDir from VOUCH_ROOT env var: %s", RootDir)
	} else {
		ex, errEx := os.Executable()
		if errEx != nil {
			panic(errEx)
		}
		RootDir = filepath.Dir(ex)
		log.Debugf("cfg.RootDir: %s", RootDir)
	}
	secretFile = filepath.Join(RootDir, "config/secret")

	// bail if we're testing
	if flag.Lookup("test.v") != nil {
		fmt.Println("`go test` detected, not loading regular config")
		return
	}

	ParseConfig()

	if Cfg.Testing {
		setDevelopmentLogger()
	}

	if *ll == "debug" || Cfg.LogLevel == "debug" {
		atom.SetLevel(zap.DebugLevel)
		log.Debug("logLevel set to debug")
	} else if *healthCheck {
		// just errors for healthcheck, unless debug is set
		atom.SetLevel(zap.ErrorLevel)
	}

	if *help {
		flag.PrintDefaults()
		os.Exit(1)
	}

	SetDefaults()

	if *port != -1 {
		Cfg.Port = *port
	}

	errT := BasicTest()
	if errT != nil {
		// log.Fatalf(errT.Error())
		panic(errT)
	}

	if *healthCheck {
		url := fmt.Sprintf("http://%s:%d/healthcheck", Cfg.Listen, Cfg.Port)
		log.Debug("Invoking healthcheck on URL ", url)
		resp, err := http.Get(url)
		if err == nil {
			robots, err := ioutil.ReadAll(resp.Body)
			resp.Body.Close()
			if err == nil {
				var result map[string]interface{}
				jsonErr := json.Unmarshal(robots, &result)
				if jsonErr == nil {
					if result["ok"] == true {
						os.Exit(0)
					}
				}
			}
		}
		log.Error("Healthcheck against ", url, " failed.")
		os.Exit(1)
	}

	var listen = Cfg.Listen + ":" + strconv.Itoa(Cfg.Port)
	if !isTCPPortAvailable(listen) {
		log.Fatal(errors.New(listen + " is not available (is " + Branding.CcName + " already running?)"))
	}

	log.Debugf("viper settings %+v", viper.AllSettings())
}

func setDevelopmentLogger() {
	// then configure the logger for development output
	logger = logger.WithOptions(
		zap.WrapCore(
			func(zapcore.Core) zapcore.Core {
				return zapcore.NewCore(zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()), zapcore.AddSync(os.Stderr), atom)
			}))
	log = logger.Sugar()
	Cfg.FastLogger = log.Desugar()
	Cfg.Logger = log
	log.Infof("testing: %s, using development console logger", strconv.FormatBool(Cfg.Testing))
}

// InitForTestPurposes is called by most *_testing.go files in Vouch Proxy
func InitForTestPurposes() {
	if err := os.Setenv(Branding.UCName+"_CONFIG", "../../config/test_config.yml"); err != nil {
		log.Error(err)
	}
	// log.Debug("opening config")
	setDevelopmentLogger()
	ParseConfig()
	SetDefaults()

}

// ParseConfig parse the config file
func ParseConfig() {
	// Precendence order: .defaults.yml < config.yml < environment variables.
	log.Debug("Loading default config")

	viper.SetConfigName(".defaults")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(filepath.Join(RootDir, "config"))
	err := viper.ReadInConfig()
	// This shouldn't actually happen unless the default is overridden!
	if err != nil {
		log.Fatalf("Fatal error reading .defaults.yml config: %s", err.Error())
		panic(err)
	}

	log.Debug("Merging additional config")

	configEnv := os.Getenv(Branding.UCName + "_CONFIG")

	if configEnv != "" {
		log.Infof("config file loaded from environmental variable %s: %s", Branding.UCName+"_CONFIG", configEnv)
		configFile, _ := filepath.Abs(configEnv)
		viper.SetConfigFile(configFile)
	} else if *cmdLineConfig != "" {
		log.Infof("config file set on commandline: %s", *cmdLineConfig)
		viper.AddConfigPath("/")
		viper.AddConfigPath(RootDir)
		viper.AddConfigPath(filepath.Join(RootDir, "config"))
		viper.SetConfigFile(*cmdLineConfig)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(filepath.Join(RootDir, "config"))
	}

	err := viper.MergeInConfig()

	if err := viper.MergeInConfig(); err != nil {
    if _, ok := err.(viper.ConfigFileNotFoundError); ok {
  		log.Warn("No additional config file found")
    } else {
  		log.Fatalf("Fatal error reading additional config file: %s", err.Error())
			panic(err)
    }
	}

	log.Debug("Reading environment variables for overrides")
	viper.AutomaticEnv()

	if err = UnmarshalKey(Branding.LCName, &Cfg); err != nil {
		log.Error(err)
	}
	if len(Cfg.Domains) == 0 {
		// then lets check for "lasso"
		var oldConfig config
		if err = UnmarshalKey(Branding.OldLCName, &oldConfig); err != nil {
			log.Error(err)
		}

		if len(oldConfig.Domains) != 0 {
			log.Errorf(`

IMPORTANT!

please update your config file to change '%s:' to '%s:' as per %s
			`, Branding.OldLCName, Branding.LCName, Branding.URL)
			Cfg = oldConfig
		}
	}

	// don't log the secret!
	// log.Debugf("secret: %s", string(Cfg.JWT.Secret))
}

// UnmarshalKey populate struct from contents of cfg tree at key
func UnmarshalKey(key string, rawVal interface{}) error {
	return viper.UnmarshalKey(key, rawVal)
}

// Get string value for key
func Get(key string) string {
	return viper.GetString(key)
}

// BasicTest just a quick sanity check to see if the config is sound
func BasicTest() error {
	if GenOAuth.Provider != Providers.Google &&
		GenOAuth.Provider != Providers.GitHub &&
		GenOAuth.Provider != Providers.IndieAuth &&
		GenOAuth.Provider != Providers.HomeAssistant &&
		GenOAuth.Provider != Providers.ADFS &&
		GenOAuth.Provider != Providers.OIDC &&
		GenOAuth.Provider != Providers.OpenStax {
		return errors.New("configuration error: Unkown oauth provider: " + GenOAuth.Provider)
	}

	for _, opt := range RequiredOptions {
		if !viper.IsSet(opt) {
			return errors.New("configuration error: required configuration option " + opt + " is not set")
		}
	}
	// Domains is required _unless_ Cfg.AllowAllUsers is set
	if !viper.IsSet(Branding.LCName+".allowAllUsers") && !viper.IsSet(Branding.LCName+".domains") {
		return fmt.Errorf("configuration error: either one of %s or %s needs to be set (but not both)", Branding.LCName+".domains", Branding.LCName+".allowAllUsers")
	}

	// OAuthconfig Checks
	switch {
	case GenOAuth.ClientID == "":
		// everyone has a clientID
		return errors.New("configuration error: oauth.client_id not found")
	case GenOAuth.Provider != Providers.IndieAuth && GenOAuth.Provider != Providers.HomeAssistant && GenOAuth.Provider != Providers.ADFS && GenOAuth.Provider != Providers.OIDC && GenOAuth.ClientSecret == "":
		// everyone except IndieAuth has a clientSecret
		// ADFS and OIDC providers also do not require this, but can have it optionally set.
		return errors.New("configuration error: oauth.client_secret not found")
	case GenOAuth.Provider != Providers.Google && GenOAuth.AuthURL == "":
		// everyone except IndieAuth and Google has an authURL
		return errors.New("configuration error: oauth.auth_url not found")
	case GenOAuth.Provider != Providers.Google && GenOAuth.Provider != Providers.IndieAuth && GenOAuth.Provider != Providers.HomeAssistant && GenOAuth.Provider != Providers.ADFS && GenOAuth.UserInfoURL == "":
		// everyone except IndieAuth, Google and ADFS has an userInfoURL
		return errors.New("configuration error: oauth.user_info_url not found")
	}

	if !viper.IsSet(Branding.LCName + ".allowAllUsers") {
		if GenOAuth.RedirectURL != "" {
			if err := checkCallbackConfig(GenOAuth.RedirectURL); err != nil {
				return err
			}
		}
		if len(GenOAuth.RedirectURLs) > 0 {
			for _, cb := range GenOAuth.RedirectURLs {
				if err := checkCallbackConfig(cb); err != nil {
					return err
				}
			}
		}
	}

	// issue a warning if the secret is too small
	log.Debugf("vouch.jwt.secret is %d characters long", len(Cfg.JWT.Secret))
	if len(Cfg.JWT.Secret) < minBase64Length {
		log.Errorf("Your secret is too short! (%d characters long). Please consider deleting %s to automatically generate a secret of %d characters",
			len(Cfg.JWT.Secret),
			Branding.LCName+".jwt.secret",
			minBase64Length)
	}

	log.Debugf("vouch.session.key is %d characters long", len(Cfg.Session.Key))
	if len(Cfg.Session.Key) < minBase64Length {
		log.Errorf("Your session key is too short! (%d characters long). Please consider deleting %s to automatically generate a secret of %d characters",
			len(Cfg.Session.Key),
			Branding.LCName+".session.key",
			minBase64Length)
	}
	if Cfg.Cookie.MaxAge < 0 {
		return fmt.Errorf("configuration error: cookie maxAge cannot be lower than 0 (currently: %d)", Cfg.Cookie.MaxAge)
	}
	if Cfg.JWT.MaxAge <= 0 {
		return fmt.Errorf("configuration error: JWT maxAge cannot be zero or lower (currently: %d)", Cfg.JWT.MaxAge)
	}
	if Cfg.Cookie.MaxAge > Cfg.JWT.MaxAge {
		return fmt.Errorf("configuration error: Cookie maxAge (%d) cannot be larger than the JWT maxAge (%d)", Cfg.Cookie.MaxAge, Cfg.JWT.MaxAge)
	}
	return nil
}

func checkCallbackConfig(url string) error {
	inDomain := false
	for _, d := range Cfg.Domains {
		if strings.Contains(url, d) {
			inDomain = true
			break
		}
	}
	if !inDomain {
		return fmt.Errorf("configuration error: oauth.callback_url (%s) must be within the configured domain where the cookie will be set %s", url, Cfg.Domains)
	}

	if !strings.Contains(url, "/auth") {
		return fmt.Errorf("configuration error: oauth.callback_url (%s) must contain '/auth'", url)
	}
	return nil
}

// SetDefaults set default options for some items
func SetDefaults() {

	// this should really be done by Viper up in parseConfig but..
	// nested defaults is currently *broken*
	// https://github.com/spf13/viper/issues/309
	// viper.SetDefault("listen", "0.0.0.0")
	// viper.SetDefault(Cfg.Port, 9090)
	// viper.SetDefault("Headers.SSO", "X-"+Branding.CcName+"-Token")
	// viper.SetDefault("Headers.Redirect", "X-"+Branding.CcName+"-Requested-URI")
	// viper.SetDefault("Cookie.Name", "Vouch")

	// jwt
	if !viper.IsSet(Branding.LCName + ".jwt.secret") {
		Cfg.JWT.Secret = getOrGenerateJWTSecret()
	}

	// cookie
	if !viper.IsSet(Branding.LCName + ".cookie.maxAge") {
		Cfg.Cookie.MaxAge = Cfg.JWT.MaxAge
	} else {
		// it is set!  is it bigger than jwt.maxage?
		if Cfg.Cookie.MaxAge > Cfg.JWT.MaxAge {
			log.Warnf("setting `%s.cookie.maxage` to `%s.jwt.maxage` value of %d minutes (curently set to %d minutes)", Branding.LCName, Branding.LCName, Cfg.JWT.MaxAge, Cfg.Cookie.MaxAge)
			Cfg.Cookie.MaxAge = Cfg.JWT.MaxAge
		}
	}

	// session
	if !viper.IsSet(Branding.LCName + ".session.key") {
		log.Warn("generating random session.key")
		rstr, err := securerandom.Base64OfBytes(base64Bytes)
		if err != nil {
			log.Fatal(err)
		}
		Cfg.Session.Key = rstr
	}

	// testing convenience variable
	if viper.IsSet(Branding.LCName + ".test_url") {
		Cfg.TestURLs = append(Cfg.TestURLs, Cfg.TestURL)
	}
	// TODO: probably change this name, maybe set the domain/port the webapp runs on
	if !viper.IsSet(Branding.LCName + ".webapp") {
		Cfg.WebApp = false
	}

	// OAuth defaults and client configuration
	err := UnmarshalKey("oauth", &GenOAuth)
	if err == nil {
		if GenOAuth.Provider == Providers.Google {
			setDefaultsGoogle()
			// setDefaultsGoogle also configures the OAuthClient
		} else if GenOAuth.Provider == Providers.GitHub {
			setDefaultsGitHub()
			configureOAuthClient()
		} else if GenOAuth.Provider == Providers.ADFS {
			setDefaultsADFS()
			configureOAuthClient()
		} else {
			// IndieAuth, OIDC, OpenStax
			configureOAuthClient()
		}
	}
}

func setDefaultsGoogle() {
	log.Info("configuring Google OAuth")
	GenOAuth.UserInfoURL = "https://www.googleapis.com/oauth2/v3/userinfo"
	if len(GenOAuth.Scopes) == 0 {
		// You have to select a scope from
		// https://developers.google.com/identity/protocols/googlescopes#google_sign-in
		GenOAuth.Scopes = []string{"email"}
	}
	OAuthClient = &oauth2.Config{
		ClientID:     GenOAuth.ClientID,
		ClientSecret: GenOAuth.ClientSecret,
		Scopes:       GenOAuth.Scopes,
		Endpoint:     google.Endpoint,
	}
	if GenOAuth.PreferredDomain != "" {
		log.Infof("setting Google OAuth preferred login domain param 'hd' to %s", GenOAuth.PreferredDomain)
		OAuthopts = oauth2.SetAuthURLParam("hd", GenOAuth.PreferredDomain)
	}
}

func setDefaultsADFS() {
	log.Info("configuring ADFS OAuth")
	OAuthopts = oauth2.SetAuthURLParam("resource", GenOAuth.RedirectURL) // Needed or all claims won't be included
}

func setDefaultsGitHub() {
	// log.Info("configuring GitHub OAuth")
	if GenOAuth.AuthURL == "" {
		GenOAuth.AuthURL = github.Endpoint.AuthURL
	}
	if GenOAuth.TokenURL == "" {
		GenOAuth.TokenURL = github.Endpoint.TokenURL
	}
	if GenOAuth.UserInfoURL == "" {
		GenOAuth.UserInfoURL = "https://api.github.com/user?access_token="
	}
	if len(GenOAuth.Scopes) == 0 {
		// https://github.com/vouch/vouch-proxy/issues/63
		// https://developer.github.com/apps/building-oauth-apps/understanding-scopes-for-oauth-apps/
		GenOAuth.Scopes = []string{"read:user"}
	}
}

func configureOAuthClient() {
	log.Infof("configuring %s OAuth with Endpoint %s", GenOAuth.Provider, GenOAuth.AuthURL)
	OAuthClient = &oauth2.Config{
		ClientID:     GenOAuth.ClientID,
		ClientSecret: GenOAuth.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  GenOAuth.AuthURL,
			TokenURL: GenOAuth.TokenURL,
		},
		RedirectURL: GenOAuth.RedirectURL,
		Scopes:      GenOAuth.Scopes,
	}
}

func getOrGenerateJWTSecret() string {
	b, err := ioutil.ReadFile(secretFile)
	if err == nil {
		log.Info("jwt.secret read from " + secretFile)
	} else {
		// then generate a new secret and store it in the file
		log.Debug(err)
		log.Info("jwt.secret not found in " + secretFile)
		log.Warn("generating random jwt.secret and storing it in " + secretFile)

		// make sure to create 256 bits for the secret
		// see https://github.com/vouch/vouch-proxy/issues/54
		rstr, err := securerandom.Base64OfBytes(base64Bytes)
		if err != nil {
			log.Fatal(err)
		}
		b = []byte(rstr)
		err = ioutil.WriteFile(secretFile, b, 0600)
		if err != nil {
			log.Debug(err)
		}
	}
	return string(b)
}

func isTCPPortAvailable(listen string) bool {
	log.Debug("checking availability of tcp port: " + listen)
	conn, err := net.Listen("tcp", listen)
	if err != nil {
		log.Error(err)
		return false
	}
	if err = conn.Close(); err != nil {
		log.Error(err)
	}
	return true
}
