package persist

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

const MaxSecretBytes = 16 * 1024

var safeSecretIdentifier = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type SecretStore struct {
	mu   sync.Mutex
	root string
}

func OpenSecrets(root string) (*SecretStore, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve data directory: %w", err)
	}
	if _, err := secureDirectory(filepath.Dir(absolute), filepath.Base(absolute)); err != nil {
		return nil, fmt.Errorf("secure data directory: %w", err)
	}
	secretRoot, err := secureDirectory(absolute, "secrets")
	if err != nil {
		return nil, fmt.Errorf("secure secrets directory: %w", err)
	}
	return &SecretStore{root: secretRoot}, nil
}

func (s *SecretStore) Stage(instanceID, key string, value []byte) (SecretRef, func() error, error) {
	if !validSecretIdentifier(instanceID) || !validSecretIdentifier(key) {
		return SecretRef{}, nil, errors.New("secret instance and key must use safe identifiers")
	}
	if len(value) == 0 {
		return SecretRef{}, nil, errors.New("secret must not be empty")
	}
	if len(value) > MaxSecretBytes {
		return SecretRef{}, nil, fmt.Errorf("secret exceeds %d bytes", MaxSecretBytes)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	instanceDirectory, err := secureDirectory(s.root, instanceID)
	if err != nil {
		return SecretRef{}, nil, fmt.Errorf("secure secret instance: %w", err)
	}
	keyDirectory, err := secureDirectory(instanceDirectory, key)
	if err != nil {
		return SecretRef{}, nil, fmt.Errorf("secure secret key: %w", err)
	}
	revision, err := randomRevision()
	if err != nil {
		return SecretRef{}, nil, fmt.Errorf("create secret revision: %w", err)
	}
	path := filepath.Join(keyDirectory, revision)
	if err := WriteAtomic(path, value, 0o600); err != nil {
		return SecretRef{}, nil, fmt.Errorf("write secret revision: %w", err)
	}
	ref := SecretRef{InstanceID: instanceID, Key: key, Revision: revision}
	var discardOnce sync.Once
	var discardErr error
	discard := func() error {
		discardOnce.Do(func() {
			discardErr = os.Remove(path)
			if errors.Is(discardErr, os.ErrNotExist) {
				discardErr = nil
			}
		})
		return discardErr
	}
	return ref, discard, nil
}

func (s *SecretStore) Read(ref SecretRef) ([]byte, error) {
	if !validSecretRef(ref) {
		return nil, errors.New("invalid secret reference")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	instanceDirectory, err := existingSecureDirectory(s.root, ref.InstanceID)
	if err != nil {
		return nil, err
	}
	keyDirectory, err := existingSecureDirectory(instanceDirectory, ref.Key)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(keyDirectory, ref.Revision)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return nil, errors.New("secret revision must be a 0600 regular file")
	}
	return ReadLimited(path, MaxSecretBytes)
}

func (s *SecretStore) Collect(live map[SecretRef]struct{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ref := range live {
		if !validSecretRef(ref) {
			return errors.New("live secret set contains an invalid reference")
		}
	}
	instances, err := os.ReadDir(s.root)
	if err != nil {
		return err
	}
	for _, instance := range instances {
		if !validSecretIdentifier(instance.Name()) || !instance.IsDir() || instance.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe entry in secrets directory: %s", instance.Name())
		}
		instancePath := filepath.Join(s.root, instance.Name())
		keys, err := os.ReadDir(instancePath)
		if err != nil {
			return err
		}
		for _, key := range keys {
			if !validSecretIdentifier(key.Name()) || !key.IsDir() || key.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("unsafe entry in secret instance: %s", key.Name())
			}
			keyPath := filepath.Join(instancePath, key.Name())
			revisions, err := os.ReadDir(keyPath)
			if err != nil {
				return err
			}
			for _, revision := range revisions {
				ref := SecretRef{InstanceID: instance.Name(), Key: key.Name(), Revision: revision.Name()}
				if !validSecretRef(ref) || !revision.Type().IsRegular() || revision.Type()&os.ModeSymlink != 0 {
					return fmt.Errorf("unsafe secret revision: %s", revision.Name())
				}
				if _, keep := live[ref]; !keep {
					if err := os.Remove(filepath.Join(keyPath, revision.Name())); err != nil {
						return err
					}
				}
			}
			if empty, err := directoryEmpty(keyPath); err != nil {
				return err
			} else if empty {
				if err := os.Remove(keyPath); err != nil {
					return err
				}
			}
		}
		if empty, err := directoryEmpty(instancePath); err != nil {
			return err
		} else if empty {
			if err := os.Remove(instancePath); err != nil {
				return err
			}
		}
	}
	return nil
}

func validSecretRef(ref SecretRef) bool {
	return validSecretIdentifier(ref.InstanceID) && validSecretIdentifier(ref.Key) && validSecretIdentifier(ref.Revision)
}

func validSecretIdentifier(value string) bool {
	return safeSecretIdentifier.MatchString(value)
}

func randomRevision() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func secureDirectory(parent, name string) (string, error) {
	path := filepath.Join(parent, name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0o700); err != nil {
			return "", err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("path must be a real directory")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return "", err
	}
	return path, nil
}

func existingSecureDirectory(parent, name string) (string, error) {
	path := filepath.Join(parent, name)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("secret path must be a real directory")
	}
	return path, nil
}

func directoryEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}
