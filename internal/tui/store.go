package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/passbolt/go-passbolt/api"
	"github.com/passbolt/go-passbolt/helper"
)

// PassboltStore reads folders and resources through a logged-in client.
type PassboltStore struct {
	client  *api.Client
	workers int
	skipped atomic.Int64
	mu      sync.Mutex // serializes re-login

	dirMu  sync.Mutex
	users  map[string]api.User
	groups map[string]string
}

// NewPassboltStore returns a store using client; workers bounds parallel
// metadata decryption (at least 1).
func NewPassboltStore(client *api.Client, workers int) *PassboltStore {
	return &PassboltStore{client: client, workers: max(workers, 1)}
}

// Skipped returns how many resources of unsupported types the last Load
// left out.
func (s *PassboltStore) Skipped() int { return int(s.skipped.Load()) }

// retry runs fn; on error it logs in again once and retries, which covers an
// expired session.
func (s *PassboltStore) retry(ctx context.Context, fn func() error) error {
	err := fn()
	if err == nil || ctx.Err() != nil {
		return err
	}
	s.mu.Lock()
	lerr := s.client.Login(ctx)
	s.mu.Unlock()
	if lerr != nil {
		return err
	}
	return fn()
}

// Load returns all folders and resources visible to the user, without secrets.
func (s *PassboltStore) Load(ctx context.Context) ([]Folder, []Entry, error) {
	var apiFolders []api.Folder
	var resources []api.Resource
	err := s.retry(ctx, func() error {
		var err error
		apiFolders, err = s.client.GetFolders(ctx, nil)
		if err != nil {
			return fmt.Errorf("listing folders: %w", err)
		}
		resources, err = s.client.GetResources(ctx, &api.GetResourcesOptions{ContainPermissions: true})
		if err != nil {
			return fmt.Errorf("listing resources: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	s.dirMu.Lock()
	s.users, s.groups = nil, nil // reload the directory with the next Permissions
	s.dirMu.Unlock()

	folders := make([]Folder, len(apiFolders))
	for i, f := range apiFolders {
		folders[i] = Folder{ID: f.ID, ParentID: f.FolderParentID, Name: f.Name}
	}

	entries, skipped, err := s.decryptAll(ctx, resources)
	s.skipped.Store(int64(skipped))
	return folders, entries, err
}

func (s *PassboltStore) decryptAll(ctx context.Context, resources []api.Resource) ([]Entry, int, error) {
	type result struct {
		entry Entry
		err   error
	}
	results := make([]result, len(resources))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(s.workers, max(len(resources), 1)) {
		wg.Go(func() {
			for i := range jobs {
				e, err := s.decryptMeta(ctx, resources[i])
				results[i] = result{e, err}
			}
		})
	}
	for i := range resources {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	entries := make([]Entry, 0, len(resources))
	skipped := 0
	for _, r := range results {
		switch {
		case r.err == nil:
			entries = append(entries, r.entry)
		case errors.Is(r.err, helper.ErrUnsupportedResourceType):
			skipped++
		default:
			return nil, 0, r.err
		}
	}
	return entries, skipped, nil
}

func (s *PassboltStore) decryptMeta(ctx context.Context, r api.Resource) (Entry, error) {
	rType, err := s.client.GetResourceTypeCached(ctx, r.ResourceTypeID)
	if err != nil {
		return Entry{}, fmt.Errorf("get resource type: %w", err)
	}
	e := Entry{ID: r.ID, FolderID: r.FolderParentID}
	if r.Created != nil {
		e.Created = r.Created.Time
	}
	if r.Modified != nil {
		e.Modified = r.Modified.Time
	}
	if r.Permission != nil {
		e.MyAccess = r.Permission.Type
	}
	if !strings.HasPrefix(rType.Slug, "v5-") {
		e.Name, e.Username, e.URI, e.Description = r.Name, r.Username, r.URI, r.Description
		return e, nil
	}
	_, meta, _, err := helper.GetResourceFieldMaps(s.client, r, api.Secret{}, *rType, false)
	if err != nil {
		return Entry{}, err
	}
	e.Name = helper.GetStringField(meta, "name")
	e.Username = helper.GetStringField(meta, "username")
	e.URI = helper.GetStringField(meta, "uri")
	e.Description = helper.GetStringField(meta, "description")
	return e, nil
}

// Secret fetches and decrypts the secret of one resource.
func (s *PassboltStore) Secret(ctx context.Context, resourceID string) (Secret, error) {
	var out Secret
	err := s.retry(ctx, func() error {
		resource, err := s.client.GetResource(ctx, resourceID)
		if err != nil {
			return fmt.Errorf("getting resource: %w", err)
		}
		rType, err := s.client.GetResourceTypeCached(ctx, resource.ResourceTypeID)
		if err != nil {
			return fmt.Errorf("getting resource type: %w", err)
		}
		secret, err := s.client.GetSecret(ctx, resource.ID)
		if err != nil {
			return fmt.Errorf("getting secret: %w", err)
		}
		_, _, fields, err := helper.GetResourceFieldMaps(s.client, *resource, *secret, *rType, true)
		if err != nil {
			return fmt.Errorf("decrypting secret: %w", err)
		}
		out = secretFromFields(fields)
		return nil
	})
	return out, err
}

// secretFromFields extracts the password and TOTP parameters.
func secretFromFields(fields map[string]any) Secret {
	out := Secret{Password: helper.GetStringField(fields, "password")}
	totp, ok := fields["totp"].(map[string]any)
	if !ok {
		return out
	}
	out.TOTPKey, _ = totp["secret_key"].(string)
	algorithm, _ := totp["algorithm"].(string)
	digits, hasDigits := totp["digits"].(float64)
	period, hasPeriod := totp["period"].(float64)
	out.TOTPSupported = out.TOTPKey != "" &&
		(algorithm == "" || strings.EqualFold(algorithm, "SHA1")) &&
		(!hasDigits || digits == 6) &&
		(!hasPeriod || period == 30)
	return out
}

// directory returns users and groups by id, loading them once.
func (s *PassboltStore) directory(ctx context.Context) (map[string]api.User, map[string]string, error) {
	s.dirMu.Lock()
	defer s.dirMu.Unlock()
	if s.users != nil && s.groups != nil {
		return s.users, s.groups, nil
	}
	users, err := s.client.GetUsers(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("listing users: %w", err)
	}
	groups, err := s.client.GetGroups(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("listing groups: %w", err)
	}
	s.users = make(map[string]api.User, len(users))
	for _, u := range users {
		s.users[u.ID] = u
	}
	s.groups = make(map[string]string, len(groups))
	for _, g := range groups {
		s.groups[g.ID] = g.Name
	}
	return s.users, s.groups, nil
}

// Permissions lists who has access to a resource with user and group names.
func (s *PassboltStore) Permissions(ctx context.Context, resourceID string) ([]Share, error) {
	var perms []api.Permission
	err := s.retry(ctx, func() error {
		var err error
		perms, err = s.client.GetResourcePermissions(ctx, resourceID)
		if err != nil {
			return fmt.Errorf("getting permissions: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	users, groups, err := s.directory(ctx)
	if err != nil {
		return nil, err
	}
	return sharesFromPermissions(perms, users, groups), nil
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func sharesFromPermissions(perms []api.Permission, users map[string]api.User, groups map[string]string) []Share {
	shares := make([]Share, 0, len(perms))
	for _, p := range perms {
		sh := Share{Type: p.Type}
		switch p.ARO {
		case "Group":
			sh.Group = true
			sh.Name = groups[p.AROForeignKey]
			if sh.Name == "" {
				sh.Name = "unknown group " + shortID(p.AROForeignKey)
			}
		default:
			u, ok := users[p.AROForeignKey]
			switch {
			case !ok:
				sh.Name = "unknown user " + shortID(p.AROForeignKey)
			case u.Profile != nil && strings.TrimSpace(u.Profile.FirstName+" "+u.Profile.LastName) != "":
				sh.Name = strings.TrimSpace(u.Profile.FirstName + " " + u.Profile.LastName)
				sh.Detail = u.Username
			default:
				sh.Name = u.Username
			}
		}
		shares = append(shares, sh)
	}
	sortShares(shares)
	return shares
}
