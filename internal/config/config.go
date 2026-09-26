package config

import (
	"os"
	"strings"

	"github.com/nekoimi/scrapio/internal/logger"
	"github.com/nekoimi/scrapio/internal/pkg/util"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type Config struct {
	// http服务端口
	Port int `json:"port,omitempty" mapstructure:"port"`
	// 应用配置
	App *AppConfig `json:"app,omitempty" mapstructure:"app"`
	// 日志等级
	LogLevel string `json:"log_level,omitempty" mapstructure:"log_level"`
	// 日志文件夹
	LogDir string `json:"log_dir,omitempty" mapstructure:"log_dir"`
	// 日志轮转配置
	LogRotation LogRotationConfig `json:"log_rotation" mapstructure:"log_rotation"`
	// Jwt secret
	JwtSecret string `json:"jwt_secret,omitempty" mapstructure:"jwt_secret"`
	// Aria2Config is retained for the deprecated downloader package only.
	// It is not loaded or exposed by the v2 control plane.
	Aria2 *Aria2Config `json:"aria2,omitempty" mapstructure:"aria2"`
	// CloudDriverConfig is retained for the future delivery plugin only.
	CloudDriver *CloudDriverConfig `json:"cloud_driver,omitempty" mapstructure:"cloud_driver"`
	// STRMConfig is retained for the future delivery plugin only.
	STRM *STRMConfig `json:"strm,omitempty" mapstructure:"strm"`
	// DownloadConfig is retained for the future delivery plugin only.
	Download *DownloadConfig `json:"download,omitempty" mapstructure:"download"`
	// 采集配置
	Crawler   *CrawlerConfig   `json:"crawler,omitempty" mapstructure:"crawler"`
	Retention *RetentionConfig `json:"retention,omitempty" mapstructure:"retention"`
	AIAssist  *AIAssistConfig  `json:"ai_assist,omitempty" mapstructure:"ai_assist"`
	// 数据库配置
	DB *DBConfig `json:"db,omitempty" mapstructure:"db"`
	// 调试 API 配置
	QuickAPI *QuickAPIConfig `json:"quick_api,omitempty" mapstructure:"quick_api"`
}

type LogRotationConfig struct {
	// 单个日志文件的最大大小，单位 MB
	MaxSizeMB int `json:"max_size_mb" mapstructure:"max_size_mb"`
	// 每个日志级别最多保留的历史文件数，0 表示不限制
	MaxBackups int `json:"max_backups" mapstructure:"max_backups"`
	// 历史日志最多保留天数，0 表示不限制
	MaxAgeDays int `json:"max_age_days" mapstructure:"max_age_days"`
	// 是否压缩因大小触发轮转的历史日志
	Compress bool `json:"compress" mapstructure:"compress"`
}

type AppConfig struct {
	// 外部访问地址，用于生成 strm 内的播放接口地址
	ExternalBaseURL string `json:"external_base_url,omitempty" mapstructure:"external_base_url"`
}

type Aria2Config struct {
	// jsonRpc
	JsonRpc string `json:"jsonrpc,omitempty" mapstructure:"jsonrpc"`
	// 验证token
	Secret string `json:"secret,omitempty" mapstructure:"secret"`
	// 移动文件夹
	MoveTo Aria2MoveToConfig `json:"move_to" mapstructure:"move_to"`
}

type Aria2MoveToConfig struct {
	// javdb 移动目录
	JavDBDir string `json:"javdb_dir,omitempty" mapstructure:"javdb_dir"`
}

type CloudDriverConfig struct {
	// 中间服务地址
	BaseURL string `json:"base_url,omitempty" mapstructure:"base_url"`
	// 网盘平台
	Platform string `json:"platform,omitempty" mapstructure:"platform"`
	// 浏览器 Profile ID
	ProfileID string `json:"profile_id,omitempty" mapstructure:"profile_id"`
	// 网盘保存根目录
	SaveRoot string `json:"save_root,omitempty" mapstructure:"save_root"`
	// HTTP 超时时间，单位秒
	Timeout int `json:"timeout,omitempty" mapstructure:"timeout"`
	// 轮询未完成任务的 cron 表达式
	PollCron string `json:"poll_cron,omitempty" mapstructure:"poll_cron"`
}

type STRMConfig struct {
	// 是否启用 strm 文件生成
	Enabled bool `json:"enabled,omitempty" mapstructure:"enabled"`
	// strm 文件整理根目录
	RootDir string `json:"root_dir,omitempty" mapstructure:"root_dir"`
	// 已存在时是否覆盖
	Overwrite bool `json:"overwrite,omitempty" mapstructure:"overwrite"`
}

type DownloadConfig struct {
	// 是否启用下载调度器
	Enabled bool `json:"enabled,omitempty" mapstructure:"enabled"`
	// 提交未下载磁力任务的 cron 表达式
	SubmitCron string `json:"submit_cron,omitempty" mapstructure:"submit_cron"`
	// 每轮最多提交数量
	BatchSize int `json:"batch_size,omitempty" mapstructure:"batch_size"`
	// 最大重试次数
	MaxRetry int `json:"max_retry,omitempty" mapstructure:"max_retry"`
}

type CrawlerConfig struct {
	// 启动立即执行
	ExecOnStartup bool `json:"exec_on_startup,omitempty" mapstructure:"exec_on_startup"`
	// worker数量
	WorkerNum int `json:"worker_num,omitempty" mapstructure:"worker_num"`

	// DrissionRod 设置
	DrissionRodGrpcIp   string `json:"drission_rod_grpc_ip,omitempty" mapstructure:"drission_rod_grpc_ip"`
	DrissionRodGrpcPort int    `json:"drission_rod_grpc_port,omitempty" mapstructure:"drission_rod_grpc_port"`
}

type RetentionConfig struct {
	DocumentDays int `json:"document_days" mapstructure:"document_days"`
}

// AIAssist is an opt-in manual suggestion gateway. It is never called by workers.
type AIAssistConfig struct {
	Enabled           bool   `json:"enabled" mapstructure:"enabled"`
	BaseURL           string `json:"base_url" mapstructure:"base_url"`
	APIKey            string `json:"api_key" mapstructure:"api_key"`
	Model             string `json:"model" mapstructure:"model"`
	MaxRequestsPerDay int    `json:"max_requests_per_day" mapstructure:"max_requests_per_day"`
}

// DBConfig 数据库相关配置
type DBConfig struct {
	// 数据库连接配置
	Dsn string `json:"dsn,omitempty" mapstructure:"dsn"`
}

type QuickAPIConfig struct {
	Token string `json:"token,omitempty" mapstructure:"token"`
}

func Load() *Config {
	v := viper.New()
	v.SetDefault("port", 8093)
	v.SetDefault("log_level", "debug")
	v.SetDefault("log_dir", "logs")
	v.SetDefault("log_rotation.max_size_mb", 20)
	v.SetDefault("log_rotation.max_backups", 7)
	v.SetDefault("log_rotation.max_age_days", 7)
	v.SetDefault("log_rotation.compress", true)
	v.SetDefault("jwt_secret", "abc123456")
	v.SetDefault("crawler.exec_on_startup", false)
	v.SetDefault("crawler.worker_num", 4)
	v.SetDefault("retention.document_days", 0)

	// 加载 YAML 配置文件
	loadYamlFile(v)

	v.BindEnv("log_rotation.max_size_mb")
	v.BindEnv("log_rotation.max_backups")
	v.BindEnv("log_rotation.max_age_days")
	v.BindEnv("log_rotation.compress")
	v.BindEnv("crawler.exec_on_startup")
	v.BindEnv("crawler.worker_num")
	v.BindEnv("retention.document_days")
	v.BindEnv("crawler.drission_rod_grpc_ip")
	v.BindEnv("crawler.drission_rod_grpc_port")
	v.BindEnv("db.dsn")
	v.BindEnv("quick_api.token")
	v.BindEnv("ai_assist.enabled")
	v.BindEnv("ai_assist.base_url")
	v.BindEnv("ai_assist.api_key")
	v.BindEnv("ai_assist.model")
	v.BindEnv("ai_assist.max_requests_per_day")

	// 从环境变量自动映射配置
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	cfg := new(Config)
	if err := v.Unmarshal(cfg); err != nil {
		panic(err)
	}

	logger.Initialize(cfg.LogLevel, cfg.LogDir, logger.RotationConfig{
		MaxSizeMB:  cfg.LogRotation.MaxSizeMB,
		MaxBackups: cfg.LogRotation.MaxBackups,
		MaxAgeDays: cfg.LogRotation.MaxAgeDays,
		Compress:   cfg.LogRotation.Compress,
	})
	log.Infof("配置信息：\n%s", cfg)

	return cfg
}

// loadYamlFile 加载环境特定的 YAML 配置文件
// 优先级：CONFIG_FILE 环境变量（指定完整路径）> config/{APP_ENV}.yaml
// 配置文件不存在不是错误，仅格式错误会输出警告
func loadYamlFile(v *viper.Viper) {
	if configFile := os.Getenv("CONFIG_FILE"); configFile != "" {
		v.SetConfigFile(configFile)
	} else {
		env := os.Getenv("APP_ENV")
		if env == "" {
			env = "dev"
		}
		v.SetConfigName(env)
		v.SetConfigType("yaml")
		v.AddConfigPath("config")
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			log.Warnf("读取配置文件异常: %s", err.Error())
		}
	} else {
		log.Infof("已加载配置文件: %s", v.ConfigFileUsed())
	}
}

func (c *Config) String() string {
	return util.ToJson(c.Redacted())
}

func (c *Config) Redacted() *Config {
	if c == nil {
		return nil
	}
	safe := *c
	safe.JwtSecret = maskSecret(c.JwtSecret)
	// Delivery configuration is intentionally hidden from the v2 control plane.
	safe.Aria2 = nil
	safe.CloudDriver = nil
	safe.STRM = nil
	safe.Download = nil
	if c.AIAssist != nil {
		assist := *c.AIAssist
		assist.APIKey = maskSecret(assist.APIKey)
		safe.AIAssist = &assist
	}
	if c.DB != nil {
		database := *c.DB
		database.Dsn = maskDSN(c.DB.Dsn)
		safe.DB = &database
	}
	if c.QuickAPI != nil {
		quickAPI := *c.QuickAPI
		quickAPI.Token = maskSecret(c.QuickAPI.Token)
		safe.QuickAPI = &quickAPI
	}
	return &safe
}

func maskSecret(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return "****"
	}
	return value[:2] + "****" + value[len(value)-2:]
}

func maskDSN(value string) string {
	if value == "" {
		return ""
	}
	if at := strings.LastIndex(value, "@"); at >= 0 {
		return "****" + value[at:]
	}
	return maskSecret(value)
}
