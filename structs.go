package main

// config is the struct for the bot internal config file
var config struct {
	// CertificatePath holds a string to the path of a full cert chain in pem format
	CertificatePath string `json:"certificatePath"`

	// PrivateKeyPath holds a string to the path of the corresponding private key to the cert in pem format
	PrivateKeyPath string `json:"privateKeyPath"`

	// BotToken holds the bot token generated by discord to authenticate the bot part
	BotToken string `json:"botToken"`

	// DiscordClientID holds the discord client id generated by discord to identify the oauth part
	DiscordClientID string `json:"discordClientId"`

	// DiscordAuthSecret holds the discord authentication secret generated by discord to authenticate against oauth
	DiscordAuthSecret string `json:"discordOAuthSecret"`

	// HostURL holds the domain name (DNS)
	HostURL string `json:"domain"`

	// HTMLPath holds the path to the mainpage to serve
	HTMLPath string `json:"html"`

	// RedisConnectionString holds the redis database connection string to authenticate with
	RedisConnectionString string `json:"redis"`

	// RedirectURL holds the relative url where oauth requests get redirected to.
	// This has to be identical to your settings at the discord bot settings page.
	RedirectURL string `json:"oAuthRedirect"`

	// WebhookIDInfo writes logs to the given webhook if set up
	// WebhookIDInfo is optional
	WebhookIDInfo string `json:"webhookIdInfo"`

	// WebhookTokenInfo is the auth token for the webhook
	// WebhookTokenInfo is optional
	WebhookTokenInfo string `json:"webhookTokenInfo"`

	// WebhookIDWarning writes logs to the given webhook if set up
	// WebhookIDWarning is optional
	WebhookIDWarning string `json:"webhookIdWarning"`

	// WebhookTokenWarning is the auth token for the webhook
	// WebhookTokenWarning is optional
	WebhookTokenWarning string `json:"webhookTokenWarning"`

	// WebhookIDError writes logs to the given webhook if set up
	// WebhookIDError is optional
	WebhookIDError string `json:"webhookIdError"`

	// WebhookTokenError is the auth token for the webhook
	// WebhookTokenError is optional
	WebhookTokenError string `json:"webhookTokenError"`
}

// gw2Account holds the data returned by the gw2 api /v2/account endpoint
type gw2Account struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	World   int      `json:"world"`
	Guilds  []string `json:"guilds"`
	Access  []string `json:"access"`
	Created string   `json:"created"`

	FractalLevel int `json:"fractal_level"`
	DailyAP      int `json:"daily_ap"`
	MonthlyAP    int `json:"monthly_ap"`
	WvWRank      int `json:"wvw_rank"`
}

// worldStruct holds the world data returned by the gw2 api /v2/worlds endpoint
type worldStruct struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// tokenInfo is the struct to the gw2 api endpoint /v2/tokeninfo
type tokenInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// dashboardTemplate holds all infos about a discord servers bot settings
type dashboardTemplate struct {
	DiscordServers []serversTemplate `json:"discordServers"`
	Gw2Servers     []serversTemplate `json:"gw2Servers"`
	Accounts       []accountTemplate `json:"accounts"`
	Mode           mode              `json:"mode"`
	RenameUsers    bool              `json:"renameUsers"`
	CreateRoles    bool              `json:"createRoles"`
	AllowLinked    bool              `json:"allowLinked"`
	VerifyOnly     bool              `json:"verifyOnly"`
	DeleteLinked   bool              `json:"deleteLinked"`
}

// serversTemplate holds infos about gw2 or discord servers
type serversTemplate struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// accountTemplate holds infos about gw2 account data
type accountTemplate struct {
	Name   string `json:"name"`
	ApiKey string `json:"apiKey"`
	Active bool   `json:"active"`
}

// the mode indicates on which mode the discord server is running the bot
type mode int

const (
	_ mode = iota
	allServers
	oneServer
	userBased
)
