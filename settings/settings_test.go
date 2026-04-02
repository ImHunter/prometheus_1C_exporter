package settings

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/LazarenkoA/prometheus_1C_exporter/logger"
	"github.com/stretchr/testify/assert"

	"github.com/jarcoal/httpmock"
)

func Test_GetDeactivateAndReset(t *testing.T) {
	httpmock.Activate()
	defer httpmock.DeactivateAndReset()

	httpmock.RegisterResponder("GET", "http://localhost/DBCredentials",
		httpmock.NewStringResponder(200, `[{"Name":"hrmcorp-n17","UserName":"testUser","UserPass":"***"}]`))

	s := &Settings{
		mx: new(sync.RWMutex),
		DBCredentials: &struct {
			URL           string                `yaml:"URL"`
			User          string                `yaml:"User"`
			Password      string                `yaml:"Password"`
			TLSSkipVerify bool                  `yaml:"TLSSkipVerify"`
			Source        TypeCredentialsSource `yaml:"Source"`
		}{
			URL:           "http://localhost/DBCredentials",
			User:          "",
			Password:      "",
			TLSSkipVerify: true,
			Source:        CredentialsSourceExternal, // явно указываем режим external
		},
	}

	logger.InitLogger(s.LogDir, 4)

	ctx, cancel := context.WithCancel(context.Background())
	go s.GetDBCredentials(ctx, make(chan struct{}))

	time.Sleep(500 * time.Millisecond)
	cancel()

	assert.Equal(t, 1, len(s.bases))
	assert.Equal(t, "hrmcorp-n17", s.bases[0].Name)
	assert.Equal(t, "testUser", s.bases[0].UserName)
	assert.Equal(t, "***", s.bases[0].UserPass)
}

func Test_LoadSettings(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		s, err := LoadSettings("")
		assert.EqualError(t, err, "файл настроек \"\" не найден")
		assert.Nil(t, s)
	})
	t.Run("pass", func(t *testing.T) {
		s, err := LoadSettings("../examples_settings.yaml")
		assert.NoError(t, err)
		assert.NotNil(t, s)
	})
	t.Run("pass env", func(t *testing.T) {
		assert.NoError(t, os.Setenv("RAC_LOGIN", "test"))
		assert.NoError(t, os.Setenv("RAC_PASSWORD", "123"))

		s, err := LoadSettings("../examples_settings.yaml")
		assert.NoError(t, err)
		if assert.NotNil(t, s) {
			assert.Equal(t, "test", s.RAC.Login)
			assert.Equal(t, "123", s.RAC.Pass)
		}
	})
}

func Test_GetLogPass(t *testing.T) {
	s := &Settings{
		mx: new(sync.RWMutex),
		bases: []InfobaseCredentials{
			{
				Name:     "test",
				UserName: "user1",
				UserPass: "1111",
			},
			{
				Name:     "test2",
				UserName: "user2",
				UserPass: "2222",
			},
		},
	}

	login, pass := s.GetLogPass("test")
	assert.Equal(t, "user1", login)
	assert.Equal(t, "1111", pass)
}

// go test -fuzz=Fuzz .\settings\...
func Fuzz_GetLogPass(f *testing.F) {
	s := &Settings{
		mx: new(sync.RWMutex),
		bases: []InfobaseCredentials{
			{
				Name:     "test",
				UserName: "user1",
				UserPass: "1111",
			},
		},
	}

	f.Fuzz(func(t *testing.T, ibname string) {
		login, pass := s.GetLogPass(ibname)
		assert.Equal(t, "", login)
		assert.Equal(t, "", pass)
	})
}

func settingsPath(body string) string {
	f, _ := os.CreateTemp("", "")
	f.WriteString(body)
	f.Close()

	return f.Name()
}

func getSettings() string {
	return `Exporters:
  - Name: ClientLic
    Property:
      timerNotify: 60
  - Name: AvailablePerformance
    Property:
      timerNotify: 10
  - Name: CPU
    Property:
      timerNotify: 10
  - Name: disk
    Property:
      timerNotify: 10
  - Name: SheduleJob
    Property:
      timerNotify: 10
  - Name: Session
    Property:
      timerNotify: 60
  - Name: Connect
    Property:
      timerNotify: 60
  - Name: SessionsData
    Property:
      timerNotify: 10
  - Name: ProcData
    Property:
      processes:
        - rphost
        - ragent
        - rmngr
      timerNotify: 10

DBCredentials: # Не обязательный параметр
  URL: http://ca-fr-web-1/fresh/int/sm/hs/PTG_SysExchange/GetDatabase
  User: ""
  Password: ""

RAC:
  Path: "/opt/1C/v8.3/x86_64/rac"
  Port: "1545"      # Не обязательный параметр
  Host: "localhost" # Не обязательный параметр
  Login: ""         # Не обязательный параметр
  Pass: ""          # Не обязательный параметр

LogDir: /var/log/1c_exporter  # Если на задан логи будут писаться в каталог с исполняемым файлом
TimeRotate: 1                 # Время в часах через которое будет создаваться новый файл логов
TTLLogs: 8                    # Время жизни логов в часах
`
}

func getBadSettings() string {
	return `Exporters:
  - Name: ClientLic
    Property:
      timerNotify: 60
  Name: AvailablePerformance
   ый параметр
  Pass: ""          # Не обязательный параметр

LogDir: /var/log/1c_exporter  # Если на задан логи будут писаться в каталог с исполняемым файлом
LogLevel: 5                   # Уровень логирования от 2 до 6, где 2 - ошибка, 3 - предупреждение, 4 - информация, 5 - дебаг, 6 - трейс
TimeRotate: 1                 # Время в часах через которое будет создаваться новый файл логов
TTLLogs: 8                    # Время жизни логов в часах
`
}

// TestGetLogPass_InternalMode проверяет получение учетных данных из секретов.
func TestGetLogPass_InternalMode(t *testing.T) {
	s := &Settings{
		DBCredentials: &struct {
			URL           string                `yaml:"URL"`
			User          string                `yaml:"User"`
			Password      string                `yaml:"Password"`
			TLSSkipVerify bool                  `yaml:"TLSSkipVerify"`
			Source        TypeCredentialsSource `yaml:"Source"`
		}{
			Source: CredentialsSourceInternal,
		},
		secrets: &IBCredentials{
			IbaseDefault: &IBCred{Login: "defaultLogin", Password: "defaultPass"},
			Ibases: map[string]IBCred{
				"testDb": {Login: "testLogin", Password: "testPass"},
			},
		},
	}

	// Конкретная база
	login, pass := s.GetLogPass("testDb")
	assert.Equal(t, "testLogin", login)
	assert.Equal(t, "testPass", pass)

	// База по умолчанию
	login, pass = s.GetLogPass("unknown")
	assert.Equal(t, "defaultLogin", login)
	assert.Equal(t, "defaultPass", pass)

	// Без секретов
	s.secrets = nil
	login, pass = s.GetLogPass("any")
	assert.Equal(t, "", login)
	assert.Equal(t, "", pass)
}

// TestGetLogPass_ExternalMode проверяет получение учетных данных из внешнего источника (bases).
func TestGetLogPass_ExternalMode(t *testing.T) {
	s := &Settings{
		DBCredentials: &struct {
			URL           string                `yaml:"URL"`
			User          string                `yaml:"User"`
			Password      string                `yaml:"Password"`
			TLSSkipVerify bool                  `yaml:"TLSSkipVerify"`
			Source        TypeCredentialsSource `yaml:"Source"`
		}{
			Source: CredentialsSourceExternal,
		},
		mx: new(sync.RWMutex),
		bases: []InfobaseCredentials{
			{Name: "db1", UserName: "extUser", UserPass: "extPass"},
		},
	}

	login, pass := s.GetLogPass("db1")
	assert.Equal(t, "extUser", login)
	assert.Equal(t, "extPass", pass)

	login, pass = s.GetLogPass("missing")
	assert.Equal(t, "", login)
	assert.Equal(t, "", pass)
}

// TestRAC_LoginPass_Priority проверяет, что учетные данные RAS берутся из секретов в режиме internal,
// а иначе – из конфигурации.
func TestRAC_LoginPass_Priority(t *testing.T) {
	s := &Settings{
		RAC: &struct {
			Path  string `yaml:"Path"`
			Port  string `yaml:"Port"`
			Host  string `yaml:"Host"`
			Login string `yaml:"Login"`
			Pass  string `yaml:"Pass"`
		}{
			Login: "configLogin",
			Pass:  "configPass",
		},
		DBCredentials: &struct {
			URL           string                `yaml:"URL"`
			User          string                `yaml:"User"`
			Password      string                `yaml:"Password"`
			TLSSkipVerify bool                  `yaml:"TLSSkipVerify"`
			Source        TypeCredentialsSource `yaml:"Source"`
		}{
			Source: CredentialsSourceInternal,
		},
		secrets: &IBCredentials{
			RAS: &IBCred{Login: "secretLogin", Password: "secretPass"},
		},
	}

	// Внутренний режим – берем из секретов
	assert.Equal(t, "secretLogin", s.RAC_Login())
	assert.Equal(t, "secretPass", s.RAC_Pass())

	// Переключаем на внешний режим – должны использоваться значения из конфига
	s.DBCredentials.Source = CredentialsSourceExternal
	assert.Equal(t, "configLogin", s.RAC_Login())
	assert.Equal(t, "configPass", s.RAC_Pass())

	// Внутренний, но секретов нет – используем конфиг
	s.DBCredentials.Source = CredentialsSourceInternal
	s.secrets = nil
	assert.Equal(t, "configLogin", s.RAC_Login())
	assert.Equal(t, "configPass", s.RAC_Pass())
}
