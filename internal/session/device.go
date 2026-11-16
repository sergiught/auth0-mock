package session

import (
	"crypto/rand"
	"encoding/base64"
	"slices"
	"strings"
	"time"
)

// DeviceStatus is where a device authorization entry stands.
type DeviceStatus int

// The states of a device authorization entry.
const (
	DevicePending DeviceStatus = iota
	DeviceApproved
	DeviceDenied
)

// Device is one RFC 8628 device authorization entry.
type Device struct {
	DeviceCode, UserCode, ClientID, Audience, Scope string
	Expires                                         time.Time
	Status                                          DeviceStatus
	Subject                                         string // Set on approval, or by SetDeviceStep after login.
	AuthTime                                        time.Time
}

// PollResult is the outcome of polling a device code.
type PollResult int

// The outcomes of PollDevice.
const (
	PollUnknown PollResult = iota
	PollExpired
	PollDenied
	PollPending
	PollApproved
)

// userCodeAlphabet is the RFC 8628 §6.1 suggested consonant set.
const userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"

type deviceEntry struct {
	dev  Device
	step string
}

func randomID() string {
	// The system random source does not fail on supported platforms.
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// newUserCode returns an XXXX-XXXX code unused by a live entry; caller holds s.mu.
func (s *Store) newUserCode() string {
	for {
		var sb strings.Builder
		b := make([]byte, 16)
		for n := 0; n < 8; {
			if _, err := rand.Read(b); err != nil {
				panic(err)
			}
			for _, v := range b {
				// Discard bytes >= 240 so every letter is equally likely.
				if v >= 240 || n == 8 {
					continue
				}
				if n == 4 {
					sb.WriteByte('-')
				}
				sb.WriteByte(userCodeAlphabet[int(v)%len(userCodeAlphabet)])
				n++
			}
		}
		code := sb.String()
		if _, taken := s.userCodes[code]; !taken {
			return code
		}
	}
}

// removeDevice deletes an entry and its indexes; caller holds s.mu.
func (s *Store) removeDevice(code string) {
	e, ok := s.devices[code]
	if !ok {
		return
	}
	delete(s.userCodes, e.dev.UserCode)
	if e.step != "" {
		delete(s.deviceSteps, e.step)
	}
	delete(s.devices, code)
	s.deviceOrder = slices.DeleteFunc(s.deviceOrder, func(x string) bool { return x == code })
}

// StartDevice fills in fresh device and user codes, stores a copy of d and
// returns it. It is bounded by MaxEntries, evicting the oldest entry first.
func (s *Store) StartDevice(d Device) Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.deviceOrder) >= MaxEntries {
		s.removeDevice(s.deviceOrder[0])
	}
	d.DeviceCode = randomID()
	d.UserCode = s.newUserCode()
	s.devices[d.DeviceCode] = &deviceEntry{dev: d}
	s.userCodes[d.UserCode] = d.DeviceCode
	s.deviceOrder = append(s.deviceOrder, d.DeviceCode)
	return d
}

// PollDevice judges a poll atomically. An unknown code or client mismatch is
// PollUnknown; an expired entry is PollExpired and deleted; an approved one is
// PollApproved and deleted; denied and pending entries stay.
func (s *Store) PollDevice(deviceCode, clientID string, now time.Time) (Device, PollResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.devices[deviceCode]
	if !ok || e.dev.ClientID != clientID {
		return Device{}, PollUnknown
	}
	d := e.dev
	if !now.Before(d.Expires) {
		s.removeDevice(deviceCode)
		return d, PollExpired
	}
	switch d.Status {
	case DeviceApproved:
		s.removeDevice(deviceCode)
		return d, PollApproved
	case DeviceDenied:
		return d, PollDenied
	}
	return d, PollPending
}

func normaliseUserCode(c string) string {
	c = strings.ToUpper(c)
	return strings.NewReplacer("-", "", " ", "").Replace(c)
}

// DeviceByUserCode finds a live pending entry by user code, ignoring case,
// dashes and spaces.
func (s *Store) DeviceByUserCode(userCode string, now time.Time) (Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := normaliseUserCode(userCode)
	if len(want) != 8 {
		return Device{}, false
	}
	e, ok := s.devices[s.userCodes[want[:4]+"-"+want[4:]]]
	if !ok || e.dev.Status != DevicePending || !now.Before(e.dev.Expires) {
		return Device{}, false
	}
	return e.dev, true
}

// SetDeviceStep records subject and authTime on the entry and returns a new
// random step id replacing any previous one. It reports false, changing
// nothing, unless the entry exists, is still pending and has not expired.
func (s *Store) SetDeviceStep(deviceCode, subject string, authTime, now time.Time) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.devices[deviceCode]
	if !ok || e.dev.Status != DevicePending || !now.Before(e.dev.Expires) {
		return "", false
	}
	if e.step != "" {
		delete(s.deviceSteps, e.step)
	}
	e.step = randomID()
	e.dev.Subject = subject
	e.dev.AuthTime = authTime
	s.deviceSteps[e.step] = deviceCode
	return e.step, true
}

// DeviceByStep peeks at the pending, live entry owning step.
func (s *Store) DeviceByStep(step string, now time.Time) (Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stepLocked(step, now, false)
}

// TakeDeviceStep is DeviceByStep that also clears the step id.
func (s *Store) TakeDeviceStep(step string, now time.Time) (Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stepLocked(step, now, true)
}

func (s *Store) stepLocked(step string, now time.Time, take bool) (Device, bool) {
	code, ok := s.deviceSteps[step]
	if !ok {
		return Device{}, false
	}
	e := s.devices[code]
	if e.dev.Status != DevicePending || !now.Before(e.dev.Expires) {
		return Device{}, false
	}
	if take {
		delete(s.deviceSteps, step)
		e.step = ""
	}
	return e.dev, true
}

// ApproveDevice moves a pending entry to approved for subject.
func (s *Store) ApproveDevice(deviceCode, subject string, authTime time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.devices[deviceCode]
	if !ok || e.dev.Status != DevicePending {
		return false
	}
	e.dev.Status = DeviceApproved
	e.dev.Subject = subject
	e.dev.AuthTime = authTime
	return true
}

// DenyDevice moves a pending entry to denied.
func (s *Store) DenyDevice(deviceCode string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.devices[deviceCode]
	if !ok || e.dev.Status != DevicePending {
		return false
	}
	e.dev.Status = DeviceDenied
	return true
}
