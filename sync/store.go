package sync

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"os/user"
	"path/filepath"
	"time"

	"github.com/boltdb/bolt"
)

// buckets
var (
	downloadItemsBucket   = []byte("download-items")
	watchedTorrentsBucket = []byte("watched-torrents")
	defaultsBucket        = []byte("defaults")
)

// Error represents a custom error.
type Error string

// Error implements error interface.
func (e Error) Error() string { return string(e) }

const (
	ErrStateNotFound   = Error("state not found")
	ErrConfigNotFound  = Error("configuration not found")
	ErrSaveStateFailed = Error("state could not be saved")
)

// Store represents persistent storage for user configuration, states etc.
type Store struct {
	path string
	db   *bolt.DB
}

// NewStore creates a new Store.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// Open acquires database handle and creates default buckets.
func (s *Store) Open() error {
	db, err := bolt.Open(s.path, 0666, &bolt.Options{Timeout: 10 * time.Second})
	if err != nil {
		return err
	}
	s.db = db

	err = s.db.Update(func(tx *bolt.Tx) error {
		_, err = tx.CreateBucketIfNotExists(defaultsBucket)
		return err
	})
	if err != nil {
		return s.db.Close()
	}
	return nil
}

// Close releases database handle.
func (s *Store) Close() error { return s.db.Close() }

// Path returns the full path of the database file.
func (s *Store) Path() string { return s.path }

// CreateBuckets creates default buckets for the given user.
func (s *Store) CreateBuckets(forUser string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		userBkt, err := tx.CreateBucketIfNotExists([]byte(forUser))
		if err != nil {
			return err
		}

		buckets := [][]byte{
			downloadItemsBucket,
			watchedTorrentsBucket,
		}

		for _, bucket := range buckets {
			_, err = userBkt.CreateBucketIfNotExists(bucket)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// SaveState inserts or updates the given state.
func (s *Store) SaveState(state *State, forUser string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		userBkt := tx.Bucket([]byte(forUser))
		downloadsBkt := userBkt.Bucket(downloadItemsBucket)

		key := itob(state.FileID)
		var value bytes.Buffer

		err := gob.NewEncoder(&value).Encode(state)
		if err != nil {
			return err
		}

		return downloadsBkt.Put(key, value.Bytes())
	})
}

// State returns a state by the given file ID.
func (s *Store) State(id int64, forUser string) (*State, error) {
	var state State
	err := s.db.View(func(tx *bolt.Tx) error {
		userBkt := tx.Bucket([]byte(forUser))
		downloadsBkt := userBkt.Bucket(downloadItemsBucket)
		fileID := itob(id)

		value := downloadsBkt.Get(fileID)
		if value == nil {
			return ErrStateNotFound
		}

		return gob.NewDecoder(bytes.NewReader(value)).Decode(&state)
	})
	return &state, err
}

// States returns all the states in the store.
func (s *Store) States(forUser string) ([]*State, error) {
	states := make([]*State, 0)

	if forUser == "" {
		return states, nil
	}

	err := s.db.View(func(tx *bolt.Tx) error {
		userBkt := tx.Bucket([]byte(forUser))
		downloadsBkt := userBkt.Bucket(downloadItemsBucket)

		cursor := downloadsBkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var state State
			err := gob.NewDecoder(bytes.NewReader(v)).Decode(&state)
			if err != nil {
				return err
			}
			// dont include hidden downloads
			if state.IsHidden {
				continue
			}
			states = append(states, &state)
		}
		return nil
	})

	return states, err
}

// Config returns configuration of the associated user.
func (s *Store) Config(forUser string) (*Config, error) {
	if forUser == "" {
		return s.DefaultConfig()
	}

	var cfg Config
	err := s.db.View(func(tx *bolt.Tx) error {
		userBkt := tx.Bucket([]byte(forUser))

		key := []byte("config")
		value := userBkt.Get(key)

		if value == nil {
			return ErrConfigNotFound
		}

		return gob.NewDecoder(bytes.NewReader(value)).Decode(&cfg)
	})

	if err == ErrConfigNotFound {
		return s.DefaultConfig()
	}

	return &cfg, err
}

// SaveConfig stores given configuration associated with given user.
func (s *Store) SaveConfig(cfg *Config, forUser string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		userBkt := tx.Bucket([]byte(forUser))

		key := []byte("config")
		var value bytes.Buffer

		err := gob.NewEncoder(&value).Encode(cfg)
		if err != nil {
			return err
		}

		return userBkt.Put(key, value.Bytes())
	})
}

// DefaultConfig returns default configuration.
func (s *Store) DefaultConfig() (*Config, error) {
	u, err := user.Current()
	if err != nil {
		return nil, err
	}
	return &Config{
		PollInterval:        Duration(defaultPollInterval),
		DownloadTo:          filepath.Join(u.HomeDir, "putio-sync"),
		DownloadFrom:        defaultDownloadFrom,
		SegmentsPerFile:     defaultSegmentsPerFile,
		MaxParallelFiles:    defaultMaxParallelFiles,
		IsPaused:            true,
		WatchTorrentsFolder: false,
		TorrentsFolder:      "",
	}, nil
}

// CurrentUser returns the last login user.
func (s *Store) CurrentUser() (string, error) {
	var username string
	err := s.db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(defaultsBucket)
		value := bkt.Get([]byte("current-user"))
		username = string(value)
		return nil
	})
	if err != nil {
		return "", err
	}
	return username, nil
}

// SaveCurrentUser stores the last login user. It is used to know which user is
// active, and whose bucket should we get.
func (s *Store) SaveCurrentUser(username string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(defaultsBucket)
		key := []byte("current-user")
		return bkt.Put(key, []byte(username))
	})
}

func itob(v int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}
