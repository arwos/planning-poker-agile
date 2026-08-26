/*
 *  Copyright (c) 2026 Mikhail Knyazhev. All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package room

import (
	"errors"
	"fmt"
	"github.com/arwos/planning-poker-agile/app/voting"
	"github.com/google/uuid"
	"strings"
	"sync"
	"unicode/utf8"
)

var DefaultCards = []float64{0, .5, 1, 2, 3, 5, 8}
var ErrNotFound = errors.New("room not found")
var ErrInvalid = errors.New("invalid room data")
var ErrCapacity = errors.New("room capacity reached")

type Participant struct {
	ID, Name, Role  string
	Lead, Submitted bool
	Selected        *float64
}
type Room struct {
	mu           sync.RWMutex
	ID           string
	Cards        []float64
	Roles        []string
	Participants map[string]*Participant
	Revealed     bool
	Average      float64
	RoleAverages map[string]float64
	Votes        map[string]float64
}

func New(cards []float64, roles []string) (*Room, error) {
	return newRoom(cards, roles, 0, 0)
}

func newRoom(cards []float64, roles []string, maxCards, maxRoles int) (*Room, error) {
	if len(cards) == 0 {
		cards = append([]float64(nil), DefaultCards...)
	}
	if maxCards > 0 && len(cards) > maxCards {
		return nil, fmt.Errorf("maximum %d story points allowed: %w", maxCards, ErrInvalid)
	}
	seen := map[float64]bool{}
	for _, c := range cards {
		if c < 0 || seen[c] {
			return nil, ErrInvalid
		}
		seen[c] = true
	}
	rseen := map[string]bool{}
	clean := []string{}
	for _, r := range roles {
		r = strings.TrimSpace(r)
		if r == "" || rseen[strings.ToLower(r)] {
			return nil, ErrInvalid
		}
		rseen[strings.ToLower(r)] = true
		clean = append(clean, r)
	}
	if len(clean) == 0 {
		return nil, ErrInvalid
	}
	if maxRoles > 0 && len(clean) > maxRoles {
		return nil, fmt.Errorf("maximum %d roles allowed: %w", maxRoles, ErrInvalid)
	}
	return &Room{ID: uuid.NewString(), Cards: cards, Roles: clean, Participants: map[string]*Participant{}, Votes: map[string]float64{}, RoleAverages: map[string]float64{}}, nil
}
func (r *Room) Add(p *Participant) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p.Name == "" || utf8.RuneCountInString(p.Name) > 40 {
		return ErrInvalid
	}
	if p.Role != "" {
		ok := false
		for _, x := range r.Roles {
			if x == p.Role {
				ok = true
			}
		}
		if !ok {
			return ErrInvalid
		}
	}
	if len(r.Participants) == 0 {
		p.Lead = true
	}
	r.Participants[p.ID] = p
	return nil
}
func (r *Room) Remove(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	wasLead := r.Participants[id] != nil && r.Participants[id].Lead
	delete(r.Participants, id)
	if wasLead {
		for _, participant := range r.Participants {
			participant.Lead = true
			break
		}
	}
	return len(r.Participants) == 0
}

func (r *Room) IsEmpty() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.Participants) == 0
}
func (r *Room) Vote(id string, value float64, submit bool) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.Participants[id]
	if !ok || p.Role == "" || r.Revealed {
		return false, ErrInvalid
	}
	valid := false
	for _, c := range r.Cards {
		if c == value {
			valid = true
		}
	}
	if !valid {
		return false, ErrInvalid
	}
	p.Selected = &value
	if submit {
		p.Submitted = true
		r.Votes[id] = value
	}
	complete := len(r.Votes) > 0
	for _, x := range r.Participants {
		if x.Role != "" && !x.Submitted {
			complete = false
		}
	}
	if complete {
		r.Revealed = true
		vals := []float64{}
		byRole := map[string][]float64{}
		for _, v := range r.Votes {
			vals = append(vals, v)
		}
		for participantID, vote := range r.Votes {
			byRole[r.Participants[participantID].Role] = append(byRole[r.Participants[participantID].Role], vote)
		}
		r.Average = voting.Average(vals)
		r.RoleAverages = map[string]float64{}
		for role, values := range byRole {
			r.RoleAverages[role] = voting.Average(values)
		}
	}
	return complete, nil
}
func (r *Room) Reset(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.Participants[id]
	if p == nil || !p.Lead {
		return ErrInvalid
	}
	r.Revealed = false
	r.Average = 0
	r.Votes = map[string]float64{}
	r.RoleAverages = map[string]float64{}
	for _, x := range r.Participants {
		x.Submitted = false
		x.Selected = nil
	}
	return nil
}
func (r *Room) Snapshot() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ps := []map[string]any{}
	for _, p := range r.Participants {
		m := map[string]any{"id": p.ID, "name": p.Name, "role": p.Role, "lead": p.Lead, "submitted": p.Submitted}
		if r.Revealed && p.Selected != nil {
			m["vote"] = *p.Selected
		}
		ps = append(ps, m)
	}
	return map[string]any{"id": r.ID, "cards": r.Cards, "roles": r.Roles, "participants": ps, "revealed": r.Revealed, "average": r.Average, "roleAverages": r.RoleAverages}
}
func (r *Room) String() string { return fmt.Sprintf("room %s", r.ID) }

type Registry struct {
	mu       sync.RWMutex
	rooms    map[string]*Room
	maxRooms int
	maxRoles int
	maxCards int
}

func NewRegistry(maxRooms int, limits ...int) *Registry {
	registry := &Registry{rooms: map[string]*Room{}, maxRooms: maxRooms}
	if len(limits) > 0 {
		registry.maxRoles = limits[0]
	}
	if len(limits) > 1 {
		registry.maxCards = limits[1]
	}
	return registry
}
func (x *Registry) Create(c []float64, roles []string) (*Room, error) {
	r, e := newRoom(c, roles, x.maxCards, x.maxRoles)
	if e == nil {
		x.mu.Lock()
		if x.maxRooms > 0 && len(x.rooms) >= x.maxRooms {
			x.mu.Unlock()
			return nil, ErrCapacity
		}
		x.rooms[r.ID] = r
		x.mu.Unlock()
	}
	return r, e
}
func (x *Registry) Get(id string) (*Room, error) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	r := x.rooms[id]
	if r == nil {
		return nil, ErrNotFound
	}
	return r, nil
}
func (x *Registry) DeleteIfEmpty(id string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if r := x.rooms[id]; r != nil && r.IsEmpty() {
		delete(x.rooms, id)
	}
}
