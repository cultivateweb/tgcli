// Package cache хранит диалоги и историю чатов в локальной БД (bbolt), чтобы
// TUI и команды открывались мгновенно из кеша, пока сеть обновляет данные в фоне.
package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"go.etcd.io/bbolt"

	"github.com/cultivateweb/tgcli/internal/telegram"
)

var (
	bucketDialogs = []byte("dialogs")
	bucketHistory = []byte("history")
	keyDialogList = []byte("list")
)

// Cache — обёртка над bbolt-базой.
type Cache struct {
	db *bbolt.DB
}

// Open открывает (создаёт) БД кеша по пути path с правами 0600.
func Open(path string) (*Cache, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bbolt.Tx) error {
		for _, b := range [][]byte{bucketDialogs, bucketHistory} {
			if _, e := tx.CreateBucketIfNotExists(b); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Cache{db: db}, nil
}

// Close закрывает БД.
func (c *Cache) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

// Dialogs возвращает закешированный список диалогов (с восстановленным Peer).
func (c *Cache) Dialogs() ([]telegram.Dialog, error) {
	var out []telegram.Dialog
	err := c.db.View(func(tx *bbolt.Tx) error {
		v := tx.Bucket(bucketDialogs).Get(keyDialogList)
		if v == nil {
			return nil
		}
		return json.Unmarshal(v, &out)
	})
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Peer = out[i].Ref.InputPeer()
	}
	return out, nil
}

// SaveDialogs сохраняет список диалогов в кеш.
func (c *Cache) SaveDialogs(d []telegram.Dialog) error {
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return c.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketDialogs).Put(keyDialogList, data)
	})
}

// History возвращает закешированную историю чата по ключу peer.
func (c *Cache) History(key string) ([]telegram.HistoryMessage, error) {
	var out []telegram.HistoryMessage
	err := c.db.View(func(tx *bbolt.Tx) error {
		v := tx.Bucket(bucketHistory).Get([]byte(key))
		if v == nil {
			return nil
		}
		return json.Unmarshal(v, &out)
	})
	return out, err
}

// SaveHistory сохраняет историю чата по ключу peer.
func (c *Cache) SaveHistory(key string, h []telegram.HistoryMessage) error {
	data, err := json.Marshal(h)
	if err != nil {
		return err
	}
	return c.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucketHistory).Put([]byte(key), data)
	})
}
