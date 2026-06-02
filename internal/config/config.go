// Package config отвечает за загрузку и сохранение настроек tgcli.
//
// Конфигурация хранится в JSON-файле (по умолчанию
// $HOME/.config/tgcli/config.json). Чувствительный api_hash может попасть
// сюда же — файл создаётся с правами 0600. Состояние авторизованной сессии
// gotd хранит в отдельном файле session.json рядом с конфигом (см. SessionPath).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// Имена переменных окружения, переопределяющих значения из файла.
// Удобно, чтобы не хранить секреты в конфиге (например, в CI).
const (
	EnvAPIID   = "TGCLI_API_ID"
	EnvAPIHash = "TGCLI_API_HASH"
	EnvPhone   = "TGCLI_PHONE"
)

// Config — настройки приложения.
//
// APIID и APIHash выдаёт https://my.telegram.org для MTProto-клиента.
type Config struct {
	APIID   int    `json:"api_id,omitempty"`
	APIHash string `json:"api_hash,omitempty"`
	Phone   string `json:"phone,omitempty"`

	// path — фактический путь, откуда конфиг загружен; нужен для Save.
	// Не сериализуется.
	path string `json:"-"`
}

// ErrNoCredentials возвращается, когда api_id/api_hash не заданы ни в файле,
// ни в окружении.
var ErrNoCredentials = errors.New(
	"не заданы api_id/api_hash: получите их на https://my.telegram.org и укажите в " +
		"конфиге или через переменные " + EnvAPIID + " / " + EnvAPIHash)

// Credentials возвращает api_id/api_hash с приоритетом окружения над файлом.
func (c *Config) Credentials() (apiID int, apiHash string, err error) {
	apiID, apiHash = c.APIID, c.APIHash

	if v := os.Getenv(EnvAPIID); v != "" {
		id, perr := strconv.Atoi(v)
		if perr != nil {
			return 0, "", fmt.Errorf("%s должен быть числом: %w", EnvAPIID, perr)
		}
		apiID = id
	}
	if v := os.Getenv(EnvAPIHash); v != "" {
		apiHash = v
	}

	if apiID == 0 || apiHash == "" {
		return 0, "", ErrNoCredentials
	}
	return apiID, apiHash, nil
}

// PhoneNumber возвращает номер телефона с приоритетом окружения над файлом.
// Пустая строка означает, что номер спросят интерактивно при входе.
func (c *Config) PhoneNumber() string {
	if v := os.Getenv(EnvPhone); v != "" {
		return v
	}
	return c.Phone
}

// SessionPath — путь к файлу сессии gotd, рядом с файлом конфигурации.
func (c *Config) SessionPath() string {
	return filepath.Join(filepath.Dir(c.path), "session.json")
}

// CachePath — путь к локальной БД кеша (диалоги/сообщения), рядом с конфигом.
func (c *Config) CachePath() string {
	return filepath.Join(filepath.Dir(c.path), "cache.db")
}

// DefaultPath возвращает путь к файлу конфигурации по умолчанию.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tgcli", "config.json"), nil
}

// Load читает конфигурацию из path. Если path пуст — используется DefaultPath.
// Отсутствие файла не считается ошибкой: возвращается пустой конфиг,
// готовый к заполнению и сохранению.
func Load(path string) (*Config, error) {
	if path == "" {
		p, err := DefaultPath()
		if err != nil {
			return nil, err
		}
		path = p
	}

	cfg := &Config{path: path}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("разбор %s: %w", path, err)
	}
	cfg.path = path
	return cfg, nil
}

// Save атомарно записывает конфигурацию на диск с правами 0600.
func (c *Config) Save() error {
	if c.path == "" {
		p, err := DefaultPath()
		if err != nil {
			return err
		}
		c.path = p
	}

	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	// Пишем во временный файл и переименовываем — чтобы не оставить
	// повреждённый конфиг при сбое посреди записи.
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// Path возвращает путь к файлу, с которым связан конфиг.
func (c *Config) Path() string { return c.path }
