package server

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Credential struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	ConnectorID string            `json:"connectorId"`
	Fields      map[string]string `json:"fields"`
	CreatedAt   time.Time         `json:"createdAt"`
}

type CredentialsFile struct {
	Version     int                   `json:"version"`
	Credentials map[string]Credential `json:"credentials"`
}

type CredentialsManager struct {
	path string
	mu   sync.RWMutex
	data *CredentialsFile
}

func NewCredentialsManager(path string) *CredentialsManager {
	return &CredentialsManager{
		path: path,
		data: &CredentialsFile{
			Version:     1,
			Credentials: make(map[string]Credential),
		},
	}
}

func (m *CredentialsManager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return json.Unmarshal(data, &m.data)
}

func (m *CredentialsManager) Save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := json.MarshalIndent(m.data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.path, data, 0o600)
}

func (m *CredentialsManager) List() []Credential {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Credential, 0, len(m.data.Credentials))
	for _, c := range m.data.Credentials {
		result = append(result, c)
	}
	return result
}

func (m *CredentialsManager) Get(id string) (Credential, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	c, ok := m.data.Credentials[id]
	return c, ok
}

func (m *CredentialsManager) Add(cred Credential) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cred.ID == "" {
		cred.ID = uuid.NewString()
	}
	cred.CreatedAt = time.Now()

	m.data.Credentials[cred.ID] = cred

	data, err := json.MarshalIndent(m.data, "", "  ")
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(m.path, data, 0o600); err != nil {
		return "", err
	}

	return cred.ID, nil
}

func (m *CredentialsManager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.data.Credentials, id)

	data, err := json.MarshalIndent(m.data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.path, data, 0o600)
}
