package settings

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/LazarenkoA/prometheus_1C_exporter/logger"
	"github.com/creasty/defaults"
	"github.com/jinzhu/copier"
	"github.com/pkg/errors"
	gitlab "gitlab.com/gitlab-org/api/client-go"
	yaml "gopkg.in/yaml.v2"
)

type TypeMetricKind string

const (
	KindUndefined                      = ""
	KindSummary         TypeMetricKind = "Summary"
	KindGauge           TypeMetricKind = "Gauge"
	KindNativeHistogram TypeMetricKind = "NativeHistogram"
)

type TypeCredentialsSource string

const (
	CredentialsSourceUndefined TypeCredentialsSource = ""         // не задан (по умолчанию external)
	CredentialsSourceExternal  TypeCredentialsSource = "external" // секреты из внешнего HTTP-сервиса (бывший plain)
	CredentialsSourceInternal  TypeCredentialsSource = "internal" // секреты через GitLab или прямую установку (бывший gitlab)
)

type TypeHostLabelFrom string

type Settings struct {
	LogDir       string `yaml:"LogDir"`
	SettingsPath string

	Exporters []*struct {
		Property map[string]interface{} `yaml:"Property"`
		Name     string                 `yaml:"Name"`
	} `yaml:"Exporters"`

	DBCredentials *struct {
		URL           string                `yaml:"URL"`           // URL внешнего сервиса (для plain)
		User          string                `yaml:"User"`          // пользователь для внешнего сервиса (plain)
		Password      string                `yaml:"Password"`      // пароль для внешнего сервиса (plain)
		TLSSkipVerify bool                  `yaml:"TLSSkipVerify"` // пропускать проверку TLS (plain)
		Source        TypeCredentialsSource `yaml:"Source"`        // источник: "", "external" или "internal"
	} `yaml:"DBCredentials"`

	GitLab *GitLabSettings `yaml:"GitLab,omitempty"` // параметры GitLab, если требуется инициировать получение секретов из GitLab

	secrets            *IBCredentials
	secretsMu          sync.RWMutex
	secretsLastUpdated time.Time

	RAC *struct {
		Path  string `yaml:"Path"`
		Port  string `yaml:"Port"`
		Host  string `yaml:"Host"`
		Login string `yaml:"Login"`
		Pass  string `yaml:"Pass"`
	} `yaml:"RAC"`

	MetricKinds *struct {
		Session      []TypeMetricKind `yaml:"Session" default:"[\"Summary\"]"`
		SessionsData []TypeMetricKind `yaml:"SessionsData" default:"[\"Summary\"]" `
	} `yaml:"MetricKinds" default:"{\"Session\": [\"Summary\"], \"SessionsData\": [\"Summary\"]}"`

	Other *struct {
		MetricNamePrefix          string `yaml:"MetricNamePrefix"`
		UseExemplars              bool   `yaml:"UseExemplars" default:"false"`
		DisableMetricsCompression bool   `yaml:"DisableMetricsCompression" default:"false"`
	} `yaml:"Other"`

	mx    *sync.RWMutex         `yaml:"-"`
	bases []InfobaseCredentials `yaml:"-"`

	LogLevel int `yaml:"LogLevel" default:"4"` // Уровень логирования от 2 до 6, где 2 - ошибка, 3 - предупреждение, 4 - информация, 5 - дебаг, 6 - трейс
}

type InfobaseCredentials struct {
	Name     string `json:"Name,omitempty" yaml:"Name,omitempty"`
	UserName string `json:"UserName,omitempty" yaml:"UserName,omitempty"`
	UserPass string `json:"UserPass,omitempty" yaml:"UserPass,omitempty"`
}

// GitLabSettings – параметры подключения к GitLab
type GitLabSettings struct {
	ProjectURL  string `yaml:"ProjectURL"`            // Например, "https://gitlab.example.com/namespace/project"
	Branch      string `yaml:"Branch"`                // ветка для триггера
	SecretsFile string `yaml:"SecretsFile"`           // имя файла секретов (по умолчанию "secrets.json.enc")
	AccessToken string `yaml:"AccessToken,omitempty"` // Personal Access Token с правами api (для всех операций)

	// кэш projectID и мьютекс
	projectID   int64
	projectIDMu sync.RWMutex
}

// Структуры для секретов
type IBCred struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type IBCredentials struct {
	RAS          *IBCred           `json:"ras,omitempty"`
	IbaseDefault *IBCred           `json:"ibase_default,omitempty"`
	Ibases       map[string]IBCred `json:"ibases,omitempty"`
}

func (s *Settings) AssignFrom(src *Settings) error {
	return copier.Copy(s, src)
}

func LoadSettings(filePath string) (*Settings, error) {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("файл настроек %q не найден", filePath)
	}
	file, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения файла %q\n%v", filePath, err)
	}

	s := new(Settings)
	if err := yaml.Unmarshal(file, s); err != nil {
		return nil, fmt.Errorf("ошибка десериализации настроек: %v", err)
	}

	s.mx = new(sync.RWMutex)

	if err := defaults.Set(s); err != nil {
		return nil, errors.Wrap(err, "set default error")
	}

	// если логпас от RAS указан в env у него приоритет над конфигом
	if login := os.Getenv("RAC_LOGIN"); login != "" {
		s.RAC.Login = login
	}
	if pass := os.Getenv("RAC_PASSWORD"); pass != "" {
		s.RAC.Pass = pass
	}

	s.SettingsPath = filePath
	return s, nil
}

func (s *Settings) getLogPass(ibname string) (login, pass string) {
	s.mx.RLock()
	defer s.mx.RUnlock()

	for _, base := range s.bases {
		if strings.EqualFold(base.Name, ibname) {
			pass = base.UserPass
			login = base.UserName
			break
		}
	}

	return
}

// GetLogPass возвращает логин и пароль для базы с именем ibName
// (используется в explorers/exporterIbInfo.go)
func (s *Settings) GetLogPass(ibName string) (login, pass string) {
	if s.IsInternalSecrets() {
		s.secretsMu.RLock()
		sec := s.secrets
		s.secretsMu.RUnlock()
		if sec != nil {
			if l, p, ok := sec.getForBase(ibName); ok {
				return l, p
			}
		}
		// если секретов нет, но режим internal – возвращаем пустые строки
		return "", ""
	}
	// external – используем старый механизм
	return s.getLogPass(ibName)
}

func (s *Settings) RAC_Path() string {
	if s.RAC != nil {
		return s.RAC.Path
	}
	return ""
}

func (s *Settings) RAC_Port() string {
	if s.RAC != nil {
		return s.RAC.Port
	}
	return ""
}

func (s *Settings) RAC_Host() string {

	if s.RAC != nil {
		return s.RAC.Host
	}
	return ""
}

func (s *Settings) RAC_Login() string {
	if s.IsInternalSecrets() && s.secrets != nil && s.secrets.RAS != nil {
		return s.secrets.RAS.Login
	}
	if s.RAC != nil {
		return s.RAC.Login
	}
	return ""
}

func (s *Settings) RAC_Pass() string {
	if s.IsInternalSecrets() && s.secrets != nil && s.secrets.RAS != nil {
		return s.secrets.RAS.Password
	}
	if s.RAC != nil {
		return s.RAC.Pass
	}
	return ""
}

func (s *Settings) GetNamePrefix() string {
	if s.Other != nil {
		return s.Other.MetricNamePrefix
	}
	return ""
}

func (s *Settings) GetRASHostPort() string {

	rasHostPort := s.RAC_Host() + ":"
	rasPort := s.RAC_Port()
	if rasPort == "" {
		rasPort = "1545"
	}
	rasHostPort += rasPort
	return rasHostPort
}

func (s *Settings) GetDisableMetricsCompression() bool {
	if s.Other != nil {
		return s.Other.DisableMetricsCompression
	}
	return false
}

func (s *Settings) GetDBCredentials(ctx context.Context, cForce chan struct{}) {
	if !s.IsExternalSecrets() {
		return
	}
	if s.DBCredentials == nil || s.DBCredentials.URL == "" {
		return
	}

	get := func() {
		s.mx.Lock()
		defer s.mx.Unlock()

		logger.With("URL", s.DBCredentials.URL).Info("обращаемся к REST")
		tlsConf := &tls.Config{InsecureSkipVerify: s.DBCredentials.TLSSkipVerify}
		data, err := request(s.DBCredentials.URL, s.DBCredentials.User, s.DBCredentials.Password, tlsConf)
		if err != nil {
			logger.Error(errors.Wrap(err, "ошибка получения данных по БД"))
		}
		if err := json.Unmarshal(data, &s.bases); err != nil {
			logger.Error(errors.Wrap(err, "не удалось десериализовать данные от REST"))
		}
	}

	// таймер для периодического обновления кредов БД
	timer := time.NewTicker(time.Hour * time.Duration(rand.Intn(6)+2)) // разброс по задержке (2-8 часа), что бы не получилось так, что все экспортеры (если их несколько) разом ломануться в REST
	get()

	defer timer.Stop()
f:
	for {
		select {
		case <-cForce:
			logger.Info("Принудительно запрашиваем список баз из REST")
			get()
		case <-timer.C:
			logger.Info("Планово запрашиваем список баз из REST")
			get()
		case <-ctx.Done():
			break f
		}
	}

}

func (s *Settings) GetProperty(explorerName string, propertyName string, defaultValue interface{}) interface{} {
	if v, ok := s.GetExporters()[explorerName][propertyName]; ok {
		return v
	} else {
		return defaultValue
	}
}

func (s *Settings) GetExporters() map[string]map[string]interface{} {
	result := map[string]map[string]interface{}{}
	for _, item := range s.Exporters {
		result[item.Name] = item.Property
	}

	return result
}

func request(url, log, pass string, tlsConf *tls.Config) ([]byte, error) {
	cl := &http.Client{
		Timeout: time.Minute,
		Transport: &http.Transport{
			TLSClientConfig: tlsConf,
		},
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if log != "" {
		req.SetBasicAuth(log, pass)
	}

	if resp, err := cl.Do(req); err != nil {
		return nil, fmt.Errorf("произошла ошибка при обращении к REST: %w", err)
	} else {
		if !(resp.StatusCode >= http.StatusOK && resp.StatusCode <= http.StatusIMUsed) {
			return nil, fmt.Errorf("REST вернул код возврата %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		defer resp.Body.Close()
		return body, nil
	}
}

func (s *Settings) UpdateSecrets(sec *IBCredentials) {
	s.secretsMu.Lock()
	defer s.secretsMu.Unlock()
	s.secrets = sec
	s.secretsLastUpdated = time.Now()
}

// GetSecretsInfo возвращает информацию о текущих секретах без раскрытия паролей.
func (s *Settings) GetSecretsInfo() (hasSecrets, hasDefault, hasRas bool, bases []string, lastUpdated time.Time) {
	s.secretsMu.RLock()
	defer s.secretsMu.RUnlock()

	if s.secrets == nil {
		return false, false, false, nil, s.secretsLastUpdated
	}

	hasSecrets = true
	hasDefault = s.secrets.IbaseDefault != nil
	hasRas = s.secrets.RAS != nil

	if len(s.secrets.Ibases) > 0 {
		bases = make([]string, 0, len(s.secrets.Ibases))
		for name := range s.secrets.Ibases {
			bases = append(bases, name)
		}
	}
	return hasSecrets, hasDefault, hasRas, bases, s.secretsLastUpdated
}

// GetProjectSlug возвращает namespace/project из ProjectURL
func (s *Settings) GetProjectSlug() (string, error) {
	if s.GitLab == nil || s.GitLab.ProjectURL == "" {
		return "", errors.New("GitLab ProjectURL not set")
	}
	u, err := url.Parse(s.GitLab.ProjectURL)
	if err != nil {
		return "", errors.Wrap(err, "invalid ProjectURL")
	}
	path := strings.TrimPrefix(u.Path, "/")
	if path == "" {
		return "", errors.New("cannot extract namespace/project from URL")
	}
	return path, nil
}

// GetGitLabClient возвращает клиент GitLab
func (s *Settings) GetGitLabClient() (*gitlab.Client, error) {
	if s.GitLab == nil || s.GitLab.ProjectURL == "" {
		return nil, errors.New("GitLab not configured")
	}
	u, err := url.Parse(s.GitLab.ProjectURL)
	if err != nil {
		return nil, errors.Wrap(err, "invalid ProjectURL")
	}
	baseURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)
	client, err := gitlab.NewClient(s.GitLab.AccessToken, gitlab.WithBaseURL(baseURL))
	if err != nil {
		return nil, err
	}
	return client, nil
}

// GetProjectID получает числовой ID проекта (с кэшированием)
func (s *Settings) GetProjectID() (int, error) {
	if s.GitLab == nil {
		return 0, errors.New("GitLab not configured")
	}

	s.GitLab.projectIDMu.RLock()
	if s.GitLab.projectID != 0 {
		defer s.GitLab.projectIDMu.RUnlock()
		return int(s.GitLab.projectID), nil
	}
	s.GitLab.projectIDMu.RUnlock()

	slug, err := s.GetProjectSlug()
	if err != nil {
		return 0, err
	}

	client, err := s.GetGitLabClient()
	if err != nil {
		return 0, err
	}

	project, _, err := client.Projects.GetProject(slug, nil)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to get project %s", slug)
	}

	s.GitLab.projectIDMu.Lock()
	s.GitLab.projectID = project.ID // int64
	s.GitLab.projectIDMu.Unlock()

	return int(project.ID), nil
}

// GitlabConfigured проверяет, заполнены ли настройки GitLab (независимо от режима)
func (s *Settings) GitlabConfigured() bool {
	if s.GitLab == nil {
		return false
	}
	return s.GitLab.ProjectURL != "" && s.GitLab.Branch != "" && s.GitLab.AccessToken != ""
}

// IsInternalSecrets возвращает true, если включен режим внутреннего хранения секретов
func (s *Settings) IsInternalSecrets() bool {
	if s.DBCredentials == nil {
		return false
	}
	return s.DBCredentials.Source == CredentialsSourceInternal
}

// IsExternalSecrets возвращает true, если включен режим внешнего получения секретов
func (s *Settings) IsExternalSecrets() bool {
	if s.DBCredentials == nil {
		// если DBCredentials нет, считаем что external (по умолчанию)
		return true
	}
	// Если Source не указан или равен external, то external
	return s.DBCredentials.Source == CredentialsSourceExternal || s.DBCredentials.Source == CredentialsSourceUndefined
}

// Возвращает логин, пароль и флаг успеха.
func (c *IBCredentials) getForBase(ibName string) (login, pass string, ok bool) {
	if c == nil {
		return "", "", false
	}
	if cred, exists := c.Ibases[ibName]; exists {
		return cred.Login, cred.Password, true
	}
	if c.IbaseDefault != nil {
		return c.IbaseDefault.Login, c.IbaseDefault.Password, true
	}
	return "", "", false
}
