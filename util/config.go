package util

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"reflect"
	"regexp"
	"strconv"

	"github.com/google/go-github/github"
	"github.com/gorilla/securecookie"
)

// Cookie is a runtime generated secure cookie used for authentication
var Cookie *securecookie.SecureCookie

// WebHostURL is the public route to the semaphore server
var WebHostURL *url.URL

type DbDriver string

const (
	DbDriverMySQL    DbDriver = "mysql"
	DbDriverBolt     DbDriver = "bolt"
	DbDriverPostgres DbDriver = "postgres"
)

type DbConfig struct {
	Dialect DbDriver `json:"-"`

	Hostname string            `json:"host"`
	Username string            `json:"user"`
	Password string            `json:"pass"`
	DbName   string            `json:"name"`
	Options  map[string]string `json:"options"`
}

type ldapMappings struct {
	DN   string `json:"dn"`
	Mail string `json:"mail"`
	UID  string `json:"uid"`
	CN   string `json:"cn"`
}

type oidcEndpoint struct {
	IssuerURL   string   `json:"issuer"`
	AuthURL     string   `json:"auth"`
	TokenURL    string   `json:"token"`
	UserInfoURL string   `json:"userinfo"`
	JWKSURL     string   `json:"jwks"`
	Algorithms  []string `json:"algorithms"`
}

type oidcProvider struct {
	ClientID      string       `json:"client_id"`
	ClientSecret  string       `json:"client_secret"`
	RedirectURL   string       `json:"redirect_url"`
	Scopes        []string     `json:"scopes"`
	DisplayName   string       `json:"display_name"`
	AutoDiscovery string       `json:"provider_url"`
	Endpoint      oidcEndpoint `json:"endpoint"`
	UsernameClaim string       `json:"username_claim"`
	NameClaim     string       `json:"name_claim"`
	EmailClaim    string       `json:"email_claim"`
}

type GitClientId string

const (
	// GoGitClientId is builtin Git client. It is not require external dependencies and is preferred.
	// Use it if you don't need external SSH authorization.
	GoGitClientId GitClientId = "go_git"
	// CmdGitClientId is external Git client.
	// Default Git client. It is use external Git binary to clone repositories.
	CmdGitClientId GitClientId = "cmd_git"
)

// ConfigType mapping between Config and the json file that sets it
type ConfigType struct {
	MySQL    DbConfig `json:"mysql"`
	BoltDb   DbConfig `json:"bolt"`
	Postgres DbConfig `json:"postgres"`

	Dialect DbDriver `json:"dialect"`

	// Format `:port_num` eg, :3000
	// if : is missing it will be corrected
	Port string `json:"port"`

	// Interface ip, put in front of the port.
	// defaults to empty
	Interface string `json:"interface"`

	// semaphore stores ephemeral projects here
	TmpPath string `json:"tmp_path"`

	// SshConfigPath is a path to the custom SSH config file.
	// Default path is ~/.ssh/config.
	SshConfigPath string `json:"ssh_config_path"`

	GitClientId GitClientId `json:"git_client"`

	// web host
	WebHost string `json:"web_host"`

	// cookie hashing & encryption
	CookieHash       string `json:"cookie_hash"`
	CookieEncryption string `json:"cookie_encryption"`
	// AccessKeyEncryption is BASE64 encoded byte array used
	// for encrypting and decrypting access keys stored in database.
	AccessKeyEncryption string `json:"access_key_encryption"`

	// email alerting
	EmailAlert    bool   `json:"email_alert"`
	EmailSender   string `json:"email_sender"`
	EmailHost     string `json:"email_host"`
	EmailPort     string `json:"email_port"`
	EmailUsername string `json:"email_username"`
	EmailPassword string `json:"email_password"`
	EmailSecure   bool   `json:"email_secure"`

	// ldap settings
	LdapEnable       bool         `json:"ldap_enable"`
	LdapBindDN       string       `json:"ldap_binddn"`
	LdapBindPassword string       `json:"ldap_bindpassword"`
	LdapServer       string       `json:"ldap_server"`
	LdapSearchDN     string       `json:"ldap_searchdn"`
	LdapSearchFilter string       `json:"ldap_searchfilter"`
	LdapMappings     ldapMappings `json:"ldap_mappings"`
	LdapNeedTLS      bool         `json:"ldap_needtls"`

	// telegram and slack alerting
	TelegramAlert bool   `json:"telegram_alert"`
	TelegramChat  string `json:"telegram_chat"`
	TelegramToken string `json:"telegram_token"`
	SlackAlert    bool   `json:"slack_alert"`
	SlackUrl      string `json:"slack_url"`

	// oidc settings
	OidcProviders map[string]oidcProvider `json:"oidc_providers"`

	// task concurrency
	MaxParallelTasks int `json:"max_parallel_tasks"`

	// feature switches
	DemoMode                 bool `json:"demo_mode"` // Deprecated, will be deleted soon
	PasswordLoginDisable     bool `json:"password_login_disable"`
	NonAdminCanCreateProject bool `json:"non_admin_can_create_project"`
}

// Config exposes the application configuration storage for use in the application
var Config *ConfigType

var (
	// default config values
	configDefaults = map[string]interface{}{
		"Port":        ":3000",
		"TmpPath":     "/tmp/semaphore",
		"GitClientId": GoGitClientId,
	}

	// mapping internal config to env-vars
	// todo: special cases - SEMAPHORE_DB_PORT, SEMAPHORE_DB_PATH (bolt), SEMAPHORE_CONFIG_PATH, OPENID for 1 provider if it makes sense
	ConfigEnvironmentalVars = map[string]string{
		"Dialect":             "SEMAPHORE_DB_DIALECT",
		"MySQL.Hostname":      "SEMAPHORE_DB_HOST",
		"MySQL.Username":      "SEMAPHORE_DB_USER",
		"MySQL.Password":      "SEMAPHORE_DB_PASS",
		"MySQL.DbName":        "SEMAPHORE_DB",
		"Postgres.Hostname":   "SEMAPHORE_DB_HOST",
		"Postgres.Username":   "SEMAPHORE_DB_USER",
		"Postgres.Password":   "SEMAPHORE_DB_PASS",
		"Postgres.DbName":     "SEMAPHORE_DB",
		"BoltDb.Hostname":     "SEMAPHORE_DB_HOST",
		"Port":                "SEMAPHORE_PORT",
		"Interface":           "SEMAPHORE_INTERFACE",
		"TmpPath":             "SEMAPHORE_TMP_PATH",
		"SshConfigPath":       "SEMAPHORE_TMP_PATH",
		"GitClientId":         "SEMAPHORE_GIT_CLIENT",
		"WebHost":             "SEMAPHORE_WEB_ROOT",
		"CookieHash":          "SEMAPHORE_COOKIE_HASH",
		"CookieEncryption":    "SEMAPHORE_COOKIE_ENCRYPTION",
		"AccessKeyEncryption": "SEMAPHORE_ACCESS_KEY_ENCRYPTION",
		"EmailAlert":          "SEMAPHORE_EMAIL_ALERT",
		"EmailSender":         "SEMAPHORE_EMAIL_SENDER",
		"EmailHost":           "SEMAPHORE_EMAIL_HOST",
		"EmailPort":           "SEMAPHORE_EMAIL_PORT",
		"EmailUsername":       "SEMAPHORE_EMAIL_USER",
		"EmailPassword":       "SEMAPHORE_EMAIL_PASSWORD",
		"EmailSecure":         "SEMAPHORE_EMAIL_SECURE",
		"LdapEnable":          "SEMAPHORE_LDAP_ACTIVATED",
		"LdapBindDN":          "SEMAPHORE_LDAP_DN_BIND",
		"LdapBindPassword":    "SEMAPHORE_LDAP_PASSWORD",
		"LdapServer":          "SEMAPHORE_LDAP_HOST",
		"LdapSearchDN":        "SEMAPHORE_LDAP_DN_SEARCH",
		"LdapSearchFilter":    "SEMAPHORE_LDAP_SEARCH_FILTER",
		"LdapMappings.DN":     "SEMAPHORE_LDAP_MAPPING_DN",
		"LdapMappings.UID":    "SEMAPHORE_LDAP_MAPPING_USERNAME",
		"LdapMappings.CN":     "SEMAPHORE_LDAP_MAPPING_FULLNAME",
		"LdapMappings.Mail":   "SEMAPHORE_LDAP_MAPPING_EMAIL",
		"LdapNeedTLS":         "SEMAPHORE_LDAP_NEEDTLS",
		"TelegramAlert":       "SEMAPHORE_TELEGRAM_ALERT",
		"TelegramChat":        "SEMAPHORE_TELEGRAM_CHAT",
		"TelegramToken":       "SEMAPHORE_TELEGRAM_TOKEN",
		"SlackAlert":          "SEMAPHORE_SLACK_ALERT",
		"SlackUrl":            "SEMAPHORE_SLACK_URL",
		"MaxParallelTasks":    "SEMAPHORE_MAX_PARALLEL_TASKS",
	}

	// basic config validation using regex
	/* NOTE: other basic regex could be used:
	   ipv4: ^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$
	   ipv6: ^(?:[A-Fa-f0-9]{1,4}:|:){3,7}[A-Fa-f0-9]{1,4}$
	   domain: ^([a-zA-Z0-9]+(-[a-zA-Z0-9]+)*\.)+[a-zA-Z]{2,}$
	   path+filename: ^([\\/[a-zA-Z0-9_\\-${}:~]*]*\\/)?[a-zA-Z0-9\\.~_${}\\-:]*$
	   email address: ^(|.*@[A-Za-z0-9-\\.]*)$
	*/
	configValidationRegex = map[string]string{
		"Dialect":             "^mysql|bolt|postgres$",
		"Port":                "^:([0-9]{1,5})$", // can have false-negatives
		"GitClientId":         "^go_git|cmd_git$",
		"CookieHash":          "^[-A-Za-z0-9+=\\/]{40,}$", // base64
		"CookieEncryption":    "^[-A-Za-z0-9+=\\/]{40,}$", // base64
		"AccessKeyEncryption": "^[-A-Za-z0-9+=\\/]{40,}$", // base64
		"EmailPort":           "^(|[0-9]{1,5})$",          // can have false-negatives
		"MaxParallelTasks":    "^[0-9]{1,10}$",            // 0-9999999999
	}
)

// ToJSON returns a JSON string of the config
func (conf *ConfigType) ToJSON() ([]byte, error) {
	return json.MarshalIndent(&conf, " ", "\t")
}

// ConfigInit reads in cli flags, and switches actions appropriately on them
func ConfigInit(configPath string) {
	fmt.Println("Loading config")
	loadConfigFile(configPath)
	loadConfigEnvironment()
	loadConfigDefaults()

	fmt.Println("Validating config")
	validateConfig(exitOnConfigError)

	var encryption []byte

	hash, _ := base64.StdEncoding.DecodeString(Config.CookieHash)
	if len(Config.CookieEncryption) > 0 {
		encryption, _ = base64.StdEncoding.DecodeString(Config.CookieEncryption)
	}

	Cookie = securecookie.New(hash, encryption)
	WebHostURL, _ = url.Parse(Config.WebHost)
	if len(WebHostURL.String()) == 0 {
		WebHostURL = nil
	}
}

func loadConfigFile(configPath string) {
	if configPath == "" {
		configPath = os.Getenv("SEMAPHORE_CONFIG_PATH")
	}

	//If the configPath option has been set try to load and decode it
	//var usedPath string

	if configPath == "" {
		cwd, err := os.Getwd()
		exitOnConfigFileError(err)
		paths := []string{
			path.Join(cwd, "config.json"),
			"/usr/local/etc/semaphore/config.json",
		}
		for _, p := range paths {
			_, err = os.Stat(p)
			if err != nil {
				continue
			}
			var file *os.File
			file, err = os.Open(p)
			if err != nil {
				continue
			}
			decodeConfig(file)
			break
		}
		exitOnConfigFileError(err)
	} else {
		p := configPath
		file, err := os.Open(p)
		exitOnConfigFileError(err)
		decodeConfig(file)
	}
}

func loadConfigDefaults() {

	for attribute, defaultValue := range configDefaults {
		if len(getConfigValue(attribute)) == 0 {
			setConfigValue(attribute, defaultValue)
		}
	}

}

func castStringToInt(value string) int {

	valueInt, err := strconv.Atoi(value)
	if err != nil {
		panic(err)
	}
	return valueInt

}

func castStringToBool(value string) bool {

	var valueBool bool
	if value == "1" || strings.ToLower(value) == "true" {
		valueBool = true
	} else {
		valueBool = false
	}
	return valueBool

}

func setConfigValue(path string, value interface{}) {

	attribute := reflect.ValueOf(Config)

	for _, nested := range strings.Split(path, ".") {
		attribute = reflect.Indirect(attribute).FieldByName(nested)
	}
	if attribute.IsValid() {
		switch attribute.Kind() {
		case reflect.Int:
			if reflect.ValueOf(value).Kind() != reflect.Int {
				value = castStringToInt(fmt.Sprintf("%v", reflect.ValueOf(value)))
			}
		case reflect.Bool:
			if reflect.ValueOf(value).Kind() != reflect.Bool {
				value = castStringToBool(fmt.Sprintf("%v", reflect.ValueOf(value)))
			}
		}
		attribute.Set(reflect.ValueOf(value))
	}

}

func getConfigValue(path string) string {

	attribute := reflect.ValueOf(Config)
	nested_path := strings.Split(path, ".")

	for i, nested := range nested_path {
		attribute = reflect.Indirect(attribute).FieldByName(nested)
		lastDepth := len(nested_path) == i+1
		if !lastDepth && attribute.Kind() != reflect.Struct || lastDepth && attribute.Kind() == reflect.Invalid {
			panic(fmt.Errorf("got non-existent config attribute '%v'", path))
		}
	}

	return fmt.Sprintf("%v", attribute)
}

func validateConfig(errorFunc func(string)) {

	if !strings.HasPrefix(Config.Port, ":") {
		Config.Port = ":" + Config.Port
	}

	for attribute, validateRegex := range configValidationRegex {
		value := getConfigValue(attribute)
		match, _ := regexp.MatchString(validateRegex, value)
		if !match {
			if !strings.Contains(attribute, "assword") && !strings.Contains(attribute, "ecret") {
				errorFunc(fmt.Sprintf(
					"value of setting '%v' is not valid: '%v' (Must match regex: '%v')",
					attribute, value, validateRegex,
				))
			} else {
				errorFunc(fmt.Sprintf(
					"value of setting '%v' is not valid! (Must match regex: '%v')",
					attribute, validateRegex,
				))
			}
		}
	}

}

func loadConfigEnvironment() {

	for attribute, envVar := range ConfigEnvironmentalVars {
		// skip unused db-dialects as they use the same env-vars
		if strings.Contains(attribute, "MySQL") && Config.Dialect != DbDriverMySQL {
			continue
		} else if strings.Contains(attribute, "Postgres") && Config.Dialect != DbDriverPostgres {
			continue
		} else if strings.Contains(attribute, "BoldDb") && Config.Dialect != DbDriverBolt {
			continue
		}

		envValue, exists := os.LookupEnv(envVar)
		if exists && len(envValue) > 0 {
			setConfigValue(attribute, envValue)
		}
	}

}

func exitOnConfigError(msg string) {
	fmt.Println(msg)
	os.Exit(1)
}

func exitOnConfigFileError(err error) {
	if err != nil {
		exitOnConfigError("Cannot Find configuration! Use --config parameter to point to a JSON file generated by `semaphore setup`.")
	}
}

func decodeConfig(file io.Reader) {
	if err := json.NewDecoder(file).Decode(&Config); err != nil {
		fmt.Println("Could not decode configuration!")
		panic(err)
	}
}

func mapToQueryString(m map[string]string) (str string) {
	for option, value := range m {
		if str != "" {
			str += "&"
		}
		str += option + "=" + value
	}
	if str != "" {
		str = "?" + str
	}
	return
}

// FindSemaphore looks in the PATH for the semaphore variable
// if not found it will attempt to find the absolute path of the first
// os argument, the semaphore command, and return it
func FindSemaphore() string {
	cmdPath, _ := exec.LookPath("semaphore") //nolint: gas

	if len(cmdPath) == 0 {
		cmdPath, _ = filepath.Abs(os.Args[0]) // nolint: gas
	}

	return cmdPath
}

func AnsibleVersion() string {
	bytes, err := exec.Command("ansible", "--version").Output()
	if err != nil {
		return ""
	}
	return string(bytes)
}

// CheckUpdate uses the GitHub client to check for new tags in the semaphore repo
func CheckUpdate() (updateAvailable *github.RepositoryRelease, err error) {
	// fetch releases
	gh := github.NewClient(nil)
	releases, _, err := gh.Repositories.ListReleases(context.TODO(), "ansible-semaphore", "semaphore", nil)
	if err != nil {
		return
	}

	updateAvailable = nil
	if (*releases[0].TagName)[1:] != Version {
		updateAvailable = releases[0]
	}

	return
}

// String returns dialect name for GORP.
func (d DbDriver) String() string {
	return string(d)
}

func (d *DbConfig) IsPresent() bool {
	return d.GetHostname() != ""
}

func (d *DbConfig) HasSupportMultipleDatabases() bool {
	return true
}

func (d *DbConfig) GetDbName() string {
	dbName := os.Getenv("SEMAPHORE_DB_NAME")
	if dbName != "" {
		return dbName
	}
	return d.DbName
}

func (d *DbConfig) GetUsername() string {
	username := os.Getenv("SEMAPHORE_DB_USER")
	if username != "" {
		return username
	}
	return d.Username
}

func (d *DbConfig) GetPassword() string {
	password := os.Getenv("SEMAPHORE_DB_PASS")
	if password != "" {
		return password
	}
	return d.Password
}

func (d *DbConfig) GetHostname() string {
	hostname := os.Getenv("SEMAPHORE_DB_HOST")
	if hostname != "" {
		return hostname
	}
	return d.Hostname
}

func (d *DbConfig) GetConnectionString(includeDbName bool) (connectionString string, err error) {
	dbName := d.GetDbName()
	dbUser := d.GetUsername()
	dbPass := d.GetPassword()
	dbHost := d.GetHostname()

	switch d.Dialect {
	case DbDriverBolt:
		connectionString = dbHost
	case DbDriverMySQL:
		if includeDbName {
			connectionString = fmt.Sprintf(
				"%s:%s@tcp(%s)/%s",
				dbUser,
				dbPass,
				dbHost,
				dbName)
		} else {
			connectionString = fmt.Sprintf(
				"%s:%s@tcp(%s)/",
				dbUser,
				dbPass,
				dbHost)
		}
		options := map[string]string{
			"parseTime":         "true",
			"interpolateParams": "true",
		}
		for v, k := range d.Options {
			options[v] = k
		}
		connectionString += mapToQueryString(options)
	case DbDriverPostgres:
		if includeDbName {
			connectionString = fmt.Sprintf(
				"postgres://%s:%s@%s/%s",
				dbUser,
				url.QueryEscape(dbPass),
				dbHost,
				dbName)
		} else {
			connectionString = fmt.Sprintf(
				"postgres://%s:%s@%s",
				dbUser,
				url.QueryEscape(dbPass),
				dbHost)
		}
		connectionString += mapToQueryString(d.Options)
	default:
		err = fmt.Errorf("unsupported database driver: %s", d.Dialect)
	}
	return
}

func (conf *ConfigType) PrintDbInfo() {
	dialect, err := conf.GetDialect()
	if err != nil {
		panic(err)
	}
	switch dialect {
	case DbDriverMySQL:
		fmt.Printf("MySQL %v@%v %v\n", conf.MySQL.GetUsername(), conf.MySQL.GetHostname(), conf.MySQL.GetDbName())
	case DbDriverBolt:
		fmt.Printf("BoltDB %v\n", conf.BoltDb.GetHostname())
	case DbDriverPostgres:
		fmt.Printf("Postgres %v@%v %v\n", conf.Postgres.GetUsername(), conf.Postgres.GetHostname(), conf.Postgres.GetDbName())
	default:
		panic(fmt.Errorf("database configuration not found"))
	}
}

func (conf *ConfigType) GetDialect() (dialect DbDriver, err error) {
	if conf.Dialect == "" {
		switch {
		case conf.MySQL.IsPresent():
			dialect = DbDriverMySQL
		case conf.BoltDb.IsPresent():
			dialect = DbDriverBolt
		case conf.Postgres.IsPresent():
			dialect = DbDriverPostgres
		default:
			err = errors.New("database configuration not found")
		}
		return
	}

	dialect = conf.Dialect
	return
}

func (conf *ConfigType) GetDBConfig() (dbConfig DbConfig, err error) {
	var dialect DbDriver
	dialect, err = conf.GetDialect()

	if err != nil {
		return
	}

	switch dialect {
	case DbDriverBolt:
		dbConfig = conf.BoltDb
	case DbDriverPostgres:
		dbConfig = conf.Postgres
	case DbDriverMySQL:
		dbConfig = conf.MySQL
	default:
		err = errors.New("database configuration not found")
	}

	dbConfig.Dialect = dialect

	return
}

// GenerateSecrets generates cookie secret during setup
func (conf *ConfigType) GenerateSecrets() {
	hash := securecookie.GenerateRandomKey(32)
	encryption := securecookie.GenerateRandomKey(32)
	accessKeyEncryption := securecookie.GenerateRandomKey(32)

	conf.CookieHash = base64.StdEncoding.EncodeToString(hash)
	conf.CookieEncryption = base64.StdEncoding.EncodeToString(encryption)
	conf.AccessKeyEncryption = base64.StdEncoding.EncodeToString(accessKeyEncryption)
}
