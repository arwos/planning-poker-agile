/*
 *  Copyright (c) 2026 Mikhail Knyazhev. All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package room

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/arwos/planning-poker-agile/app/voting"
	"github.com/google/uuid"
)

var DefaultCards = []float64{0, .5, 1, 2, 3, 5, 8}
var ErrNotFound = errors.New("room not found")
var ErrInvalid = errors.New("invalid room data")
var ErrCapacity = errors.New("room capacity reached")
var ErrInvalidReconnect = errors.New("invalid reconnect token")

const (
	maxParticipantNameLength = 40
	maxRoleNameLength        = 40
	maxOwnerTokenLength      = 128
	maxReconnectTokenLength  = 128
	defaultMaxRooms          = 100
	defaultMaxParticipants   = 32
	reconnectRetention       = 10 * time.Minute
)

type Participant struct {
	ID, Name, Role           string
	Lead, Submitted, Skipped bool
	Selected                 *float64
	clientID                 string
	connectionID             string
	reconnectTokenHash       string
	connected                bool
	disconnectedAt           time.Time
}

type ParticipantState struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Role      string   `json:"role"`
	Lead      bool     `json:"lead"`
	Submitted bool     `json:"submitted"`
	Skipped   bool     `json:"skipped"`
	Vote      *float64 `json:"vote,omitempty"`
}

type State struct {
	ID           string             `json:"id"`
	Cards        []float64          `json:"cards"`
	Roles        []string           `json:"roles"`
	Participants []ParticipantState `json:"participants"`
	Revealed     bool               `json:"revealed"`
	Average      float64            `json:"average"`
	HasVotes     bool               `json:"hasVotes"`
	RoleAverages map[string]float64 `json:"roleAverages"`
}
type Room struct {
	mu                 sync.RWMutex
	ID                 string
	Cards              []float64
	Roles              []string
	Participants       map[string]*Participant
	Revealed           bool
	Average            float64
	RoleAverages       map[string]float64
	Votes              map[string]float64
	hasVotes           bool
	ownerToken         string
	ownerParticipantID string
	maxParticipants    int
	reconnectIndex     map[string]string
}

func New(cards []float64, roles []string) (*Room, error) {
	return newRoom(cards, roles, 0, 0, "", defaultMaxParticipants)
}

func newRoom(cards []float64, roles []string, maxCards, maxRoles int, ownerToken string, maxParticipants ...int) (*Room, error) {
	ownerToken = strings.TrimSpace(ownerToken)
	if ownerToken != "" && len(ownerToken) > maxOwnerTokenLength {
		return nil, ErrInvalid
	}
	if len(cards) == 0 {
		cards = append([]float64(nil), DefaultCards...)
	}
	if maxCards > 0 && len(cards) > maxCards {
		return nil, fmt.Errorf("maximum %d story points allowed: %w", maxCards, ErrInvalid)
	}
	seen := map[float64]bool{}
	for _, c := range cards {
		if math.IsNaN(c) || math.IsInf(c, 0) || c < 0 || seen[c] {
			return nil, ErrInvalid
		}
		seen[c] = true
	}
	rseen := map[string]bool{}
	clean := []string{}
	for _, r := range roles {
		r = strings.TrimSpace(r)
		if !utf8.ValidString(r) || r == "" || utf8.RuneCountInString(r) > maxRoleNameLength || rseen[strings.ToLower(r)] {
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
	participantLimit := defaultMaxParticipants
	if len(maxParticipants) > 0 && maxParticipants[0] > 0 {
		participantLimit = maxParticipants[0]
	}
	return &Room{
		ID:              uuid.NewString(),
		Cards:           cards,
		Roles:           clean,
		Participants:    map[string]*Participant{},
		Votes:           map[string]float64{},
		RoleAverages:    map[string]float64{},
		ownerToken:      ownerToken,
		maxParticipants: participantLimit,
		reconnectIndex:  map[string]string{},
	}, nil
}
func (r *Room) Add(p *Participant) error {
	_, err := r.AddConnection(p, "", "", "")
	return err
}

func (r *Room) AddConnection(p *Participant, clientID, ownerToken, connectionID string) (bool, error) {
	replaced, _, err := r.addConnection(p, clientID, "", ownerToken, connectionID)
	return replaced, err
}

func (r *Room) AddConnectionWithToken(p *Participant, clientID, reconnectToken, ownerToken, connectionID string) (bool, string, error) {
	return r.addConnection(p, clientID, reconnectToken, ownerToken, connectionID)
}

func (r *Room) addConnection(p *Participant, clientID, reconnectToken, ownerToken, connectionID string) (bool, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.purgeDisconnectedLocked(time.Now())
	if p == nil || !utf8.ValidString(p.Name) || p.Name == "" || utf8.RuneCountInString(p.Name) > maxParticipantNameLength {
		return false, "", ErrInvalid
	}
	if r.Participants == nil {
		r.Participants = map[string]*Participant{}
	}
	if r.Votes == nil {
		r.Votes = map[string]float64{}
	}
	if p.Role != "" {
		ok := false
		for _, x := range r.Roles {
			if x == p.Role {
				ok = true
			}
		}
		if !ok {
			return false, "", ErrInvalid
		}
		if utf8.RuneCountInString(p.Role) > maxRoleNameLength {
			return false, "", ErrInvalid
		}
	}
	if r.reconnectIndex == nil {
		r.reconnectIndex = map[string]string{}
	}
	isOwner := r.isOwnerTokenLocked(ownerToken)
	if reconnectToken != "" {
		if len(strings.TrimSpace(reconnectToken)) > maxReconnectTokenLength {
			return false, "", ErrInvalidReconnect
		}
		hash := hashReconnectToken(reconnectToken)
		id, ok := r.reconnectIndex[hash]
		existing := r.Participants[id]
		if !ok || existing == nil || existing.clientID != clientID {
			return false, "", ErrInvalidReconnect
		}
		return r.replaceParticipantLocked(p, existing, clientID, hash, reconnectToken, connectionID, isOwner), reconnectToken, nil
	}
	if isOwner && r.ownerParticipantID != "" {
		if existing := r.Participants[r.ownerParticipantID]; existing != nil {
			token, err := newReconnectToken()
			if err != nil {
				return false, "", err
			}
			hash := hashReconnectToken(token)
			return r.replaceParticipantLocked(p, existing, clientID, hash, token, connectionID, true), token, nil
		}
	}
	if r.maxParticipants > 0 && len(r.Participants) >= r.maxParticipants {
		return false, "", ErrCapacity
	}
	token, err := newReconnectToken()
	if err != nil {
		return false, "", err
	}
	hash := hashReconnectToken(token)
	if r.activeParticipantCountLocked() == 0 {
		p.Lead = true
	}
	p.clientID = clientID
	p.connectionID = connectionID
	p.reconnectTokenHash = hash
	p.connected = true
	p.disconnectedAt = time.Time{}
	r.Participants[p.ID] = p
	r.reconnectIndex[hash] = p.ID
	if isOwner {
		r.ownerParticipantID = p.ID
		r.promoteLeadLocked(p.ID)
	}
	return false, token, nil
}

func (r *Room) replaceParticipantLocked(p, existing *Participant, clientID, hash, token, connectionID string, isOwner bool) bool {
	p.ID = existing.ID
	p.Lead = existing.Lead
	if p.Role == existing.Role {
		p.Submitted = existing.Submitted
		p.Skipped = existing.Skipped
		p.Selected = existing.Selected
	} else {
		delete(r.Votes, existing.ID)
		if !r.Revealed {
			r.hasVotes = len(r.Votes) > 0
		}
		p.Submitted = false
		p.Skipped = false
		p.Selected = nil
	}
	delete(r.reconnectIndex, existing.reconnectTokenHash)
	p.clientID = clientID
	p.connectionID = connectionID
	p.reconnectTokenHash = hash
	p.connected = true
	p.disconnectedAt = time.Time{}
	r.Participants[p.ID] = p
	r.reconnectIndex[hash] = p.ID
	if isOwner {
		r.ownerParticipantID = p.ID
		r.promoteLeadLocked(p.ID)
	}
	return true
}

func (r *Room) isOwnerTokenLocked(ownerToken string) bool {
	return ownerToken != "" && r.ownerToken != "" && subtle.ConstantTimeCompare([]byte(ownerToken), []byte(r.ownerToken)) == 1
}

func (r *Room) promoteLeadLocked(id string) {
	for participantID, participant := range r.Participants {
		if participant == nil {
			continue
		}
		participant.Lead = participantID == id
	}
}

func (r *Room) Remove(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.purgeDisconnectedLocked(time.Now())
	wasLead := r.Participants[id] != nil && r.Participants[id].Lead
	if participant := r.Participants[id]; participant != nil {
		delete(r.reconnectIndex, participant.reconnectTokenHash)
		if !r.Revealed {
			delete(r.Votes, id)
			r.hasVotes = len(r.Votes) > 0
		}
	}
	delete(r.Participants, id)
	if wasLead {
		r.promoteFirstConnectedLocked()
	}
	return r.activeParticipantCountLocked() == 0
}

func (r *Room) RemoveConnection(id, connectionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.purgeDisconnectedLocked(time.Now())
	participant := r.Participants[id]
	if participant == nil || !participant.connected || participant.connectionID != connectionID {
		return false
	}
	wasLead := participant.Lead
	if !r.Revealed {
		delete(r.Votes, id)
		r.hasVotes = len(r.Votes) > 0
		participant.Submitted = false
		participant.Skipped = false
		participant.Selected = nil
	}
	participant.connected = false
	participant.connectionID = ""
	participant.disconnectedAt = time.Now()
	participant.Lead = false
	if wasLead {
		r.promoteFirstConnectedLocked()
	}
	return true
}

func (r *Room) IsEmpty() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.activeParticipantCountLocked() == 0
}
func (r *Room) Vote(id string, value float64, submit bool) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.purgeDisconnectedLocked(time.Now())
	p, ok := r.Participants[id]
	if !ok || p == nil || !p.connected || p.Role == "" || r.Revealed {
		return false, ErrInvalid
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
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
	p.Skipped = false
	if submit {
		p.Submitted = true
		r.Votes[id] = value
		r.hasVotes = true
	} else {
		p.Submitted = false
		delete(r.Votes, id)
	}
	return r.revealIfCompleteLocked(), nil
}

func (r *Room) SkipVote(id string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.purgeDisconnectedLocked(time.Now())
	p, ok := r.Participants[id]
	if !ok || p == nil || !p.connected || p.Role == "" || r.Revealed {
		return false, ErrInvalid
	}
	p.Selected = nil
	p.Submitted = true
	p.Skipped = true
	delete(r.Votes, id)
	return r.revealIfCompleteLocked(), nil
}

func (r *Room) revealIfCompleteLocked() bool {
	hasVoter := false
	for _, participant := range r.Participants {
		if participant == nil || !participant.connected {
			continue
		}
		if participant.Role == "" {
			continue
		}
		hasVoter = true
		if !participant.Submitted {
			return false
		}
	}
	if !hasVoter {
		return false
	}
	r.Revealed = true
	vals := make([]float64, 0, len(r.Votes))
	byRole := map[string][]float64{}
	for participantID, vote := range r.Votes {
		participant := r.Participants[participantID]
		if participant == nil {
			continue
		}
		vals = append(vals, vote)
		byRole[participant.Role] = append(byRole[participant.Role], vote)
	}
	r.Average = voting.Average(vals)
	r.RoleAverages = map[string]float64{}
	for role, values := range byRole {
		r.RoleAverages[role] = voting.Average(values)
	}
	r.hasVotes = len(vals) > 0
	return true
}
func (r *Room) Reset(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.purgeDisconnectedLocked(time.Now())
	p := r.Participants[id]
	if p == nil || !p.connected || !p.Lead {
		return ErrInvalid
	}
	r.Revealed = false
	r.Average = 0
	r.Votes = map[string]float64{}
	r.RoleAverages = map[string]float64{}
	r.hasVotes = false
	for _, x := range r.Participants {
		if x == nil {
			continue
		}
		x.Submitted = false
		x.Skipped = false
		x.Selected = nil
	}
	return nil
}
func (r *Room) State() State {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ps := make([]ParticipantState, 0, len(r.Participants))
	for _, p := range r.Participants {
		if p == nil || !p.connected {
			continue
		}
		state := ParticipantState{ID: p.ID, Name: p.Name, Role: p.Role, Lead: p.Lead, Submitted: p.Submitted, Skipped: p.Skipped}
		if r.Revealed && p.Selected != nil {
			vote := *p.Selected
			state.Vote = &vote
		}
		ps = append(ps, state)
	}
	roleAverages := make(map[string]float64, len(r.RoleAverages))
	for role, average := range r.RoleAverages {
		roleAverages[role] = average
	}
	return State{ID: r.ID, Cards: r.Cards, Roles: r.Roles, Participants: ps, Revealed: r.Revealed, Average: r.Average, HasVotes: r.hasVotes, RoleAverages: roleAverages}
}

// Snapshot preserves the original Go-facing shape for domain callers. HTTP and WebSocket
// transports use State directly to avoid dynamic map allocations on every broadcast.
func (r *Room) Snapshot() map[string]any {
	state := r.State()
	participants := make([]map[string]any, 0, len(state.Participants))
	for _, p := range state.Participants {
		item := map[string]any{"id": p.ID, "name": p.Name, "role": p.Role, "lead": p.Lead, "submitted": p.Submitted, "skipped": p.Skipped}
		if p.Vote != nil {
			item["vote"] = *p.Vote
		}
		participants = append(participants, item)
	}
	return map[string]any{"id": state.ID, "cards": state.Cards, "roles": state.Roles, "participants": participants, "revealed": state.Revealed, "average": state.Average, "hasVotes": state.HasVotes, "roleAverages": state.RoleAverages}
}
func (r *Room) String() string { return fmt.Sprintf("room %s", r.ID) }

func (r *Room) activeParticipantCountLocked() int {
	count := 0
	for _, participant := range r.Participants {
		if participant != nil && participant.connected {
			count++
		}
	}
	return count
}

func (r *Room) promoteFirstConnectedLocked() {
	for participantID, participant := range r.Participants {
		if participant == nil || !participant.connected {
			continue
		}
		participant.Lead = true
		for otherID, other := range r.Participants {
			if otherID != participantID && other != nil {
				other.Lead = false
			}
		}
		return
	}
}

func (r *Room) purgeDisconnectedLocked(now time.Time) {
	for id, participant := range r.Participants {
		if participant == nil || participant.connected || participant.disconnectedAt.IsZero() || now.Sub(participant.disconnectedAt) < reconnectRetention {
			continue
		}
		delete(r.reconnectIndex, participant.reconnectTokenHash)
		delete(r.Participants, id)
		if r.ownerParticipantID == id {
			r.ownerParticipantID = ""
		}
	}
}

// PurgeDisconnected removes expired reconnect identities without a background goroutine.
func (r *Room) PurgeDisconnected(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.purgeDisconnectedLocked(now)
}

type Registry struct {
	mu              sync.RWMutex
	rooms           map[string]*Room
	maxRooms        int
	maxRoles        int
	maxCards        int
	maxParticipants int
	pending         map[string]*Reservation
}

type Reservation struct {
	ID         string
	Cards      []float64
	Roles      []string
	OwnerToken string
	ExpiresAt  time.Time
}

func NewRegistry(maxRooms int, limits ...int) *Registry {
	if maxRooms <= 0 {
		maxRooms = defaultMaxRooms
	}
	registry := &Registry{rooms: map[string]*Room{}, pending: map[string]*Reservation{}, maxRooms: maxRooms, maxParticipants: defaultMaxParticipants}
	if len(limits) > 0 {
		registry.maxRoles = limits[0]
	}
	if len(limits) > 1 {
		registry.maxCards = limits[1]
	}
	if len(limits) > 2 && limits[2] > 0 {
		registry.maxParticipants = limits[2]
	}
	return registry
}
func (x *Registry) Create(c []float64, roles []string) (*Room, error) {
	return x.create(c, roles, "")
}

func (x *Registry) CreateWithOwner(c []float64, roles []string, ownerToken string) (*Room, error) {
	return x.create(c, roles, ownerToken)
}

func (x *Registry) create(c []float64, roles []string, ownerToken string) (*Room, error) {
	r, e := newRoom(c, roles, x.maxCards, x.maxRoles, ownerToken, x.maxParticipants)
	if e == nil {
		x.mu.Lock()
		x.purgeExpiredLocked(time.Now())
		if x.roomCountLocked() >= x.maxRooms {
			x.mu.Unlock()
			return nil, ErrCapacity
		}
		x.rooms[r.ID] = r
		x.mu.Unlock()
	}
	return r, e
}

func (x *Registry) ReserveWithOwner(c []float64, roles []string, ownerToken string, ttl time.Duration) (*Reservation, error) {
	r, err := newRoom(c, roles, x.maxCards, x.maxRoles, ownerToken, x.maxParticipants)
	if err != nil {
		return nil, err
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	reservation := &Reservation{ID: r.ID, Cards: r.Cards, Roles: r.Roles, OwnerToken: ownerToken, ExpiresAt: time.Now().Add(ttl)}
	x.mu.Lock()
	defer x.mu.Unlock()
	x.purgeExpiredLocked(time.Now())
	if x.roomCountLocked() >= x.maxRooms {
		return nil, ErrCapacity
	}
	x.pending[reservation.ID] = reservation
	return reservation, nil
}

func (x *Registry) roomCountLocked() int {
	return len(x.rooms) + len(x.pending)
}

func (x *Registry) purgeExpiredLocked(now time.Time) {
	for id, reservation := range x.pending {
		if !now.Before(reservation.ExpiresAt) {
			delete(x.pending, id)
		}
	}
}

func (x *Registry) PublicState(id string) (State, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.purgeExpiredLocked(time.Now())
	if r := x.rooms[id]; r != nil {
		r.PurgeDisconnected(time.Now())
		return r.State(), nil
	}
	reservation := x.pending[id]
	if reservation == nil {
		return State{}, ErrNotFound
	}
	return State{ID: reservation.ID, Cards: reservation.Cards, Roles: reservation.Roles, Participants: []ParticipantState{}, RoleAverages: map[string]float64{}}, nil
}

func (x *Registry) Join(id string, p *Participant, clientID, reconnectToken, ownerToken, connectionID string) (*Room, bool, string, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.purgeExpiredLocked(time.Now())
	if r := x.rooms[id]; r != nil {
		replaced, token, err := r.AddConnectionWithToken(p, clientID, reconnectToken, ownerToken, connectionID)
		return r, replaced, token, err
	}
	reservation := x.pending[id]
	if reservation == nil {
		return nil, false, "", ErrNotFound
	}
	r, err := newRoom(reservation.Cards, reservation.Roles, x.maxCards, x.maxRoles, reservation.OwnerToken, x.maxParticipants)
	if err != nil {
		return nil, false, "", err
	}
	r.ID = reservation.ID
	replaced, token, err := r.AddConnectionWithToken(p, clientID, reconnectToken, ownerToken, connectionID)
	if err != nil {
		return nil, false, "", err
	}
	delete(x.pending, id)
	x.rooms[id] = r
	return r, replaced, token, nil
}

func newReconnectToken() (string, error) {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data[:]), nil
}

func hashReconnectToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return string(hash[:])
}
func (x *Registry) Get(id string) (*Room, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.purgeExpiredLocked(time.Now())
	r := x.rooms[id]
	if r == nil {
		return nil, ErrNotFound
	}
	r.PurgeDisconnected(time.Now())
	return r, nil
}
func (x *Registry) DeleteIfEmpty(id string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.purgeExpiredLocked(time.Now())
	if r := x.rooms[id]; r != nil {
		r.PurgeDisconnected(time.Now())
		if r.IsEmpty() {
			delete(x.rooms, id)
		}
	}
}
