package cognito

import (
	"log"
	"sort"
	"strings"
	"time"
)

// Code purposes. Verification codes use "verify:<attribute>".
const (
	purposeSignUp = "signup"
	purposeReset  = "reset"
	purposeInvite = "invite" // AdminCreateUser temporary password
)

const maxCodeAttempts = 5

// pendingCode is a code AWS would have emailed or texted. Tarn logs it and
// exposes it through the admin API and dashboard instead.
type pendingCode struct {
	Code      string    `json:"code"`
	Attribute string    `json:"attribute,omitempty"`
	Created   time.Time `json:"created"`
	Expires   time.Time `json:"expires"`
	Attempts  int       `json:"attempts,omitempty"`
}

func codeTTL(purpose string) time.Duration {
	if purpose == purposeSignUp {
		return 24 * time.Hour
	}
	return time.Hour
}

// issueCode stores and logs a new code for u.
func (s *Service) issueCode(p *pool, u *user, purpose, attribute string) string {
	code := newCode()
	if s.cfg.CognitoFixedCode != "" {
		code = s.cfg.CognitoFixedCode
	}
	s.storeCode(p, u, purpose, attribute, code, codeTTL(purpose))
	return code
}

func (s *Service) storeCode(p *pool, u *user, purpose, attribute, code string, ttl time.Duration) {
	now := s.now()
	if u.Codes == nil {
		u.Codes = make(map[string]*pendingCode)
	}
	u.Codes[purpose] = &pendingCode{Code: code, Attribute: attribute, Created: now, Expires: now.Add(ttl)}
	log.Printf("[cognito] code pool=%s user=%s purpose=%s code=%s", p.Config.Id, u.Username, purpose, code)
	s.markDirty()
}

// consumeCode checks code against the pending code for purpose and removes it
// on success. Five wrong attempts lock the code, as in AWS.
func (s *Service) consumeCode(u *user, purpose, code string) error {
	pc := u.Codes[purpose]
	if pc == nil {
		return errCodeMismatch
	}
	if pc.Attempts >= maxCodeAttempts {
		return errAttemptLimit
	}
	if s.now().After(pc.Expires) {
		return errExpiredCode
	}
	if pc.Code != code {
		pc.Attempts++
		s.markDirty()
		return errCodeMismatch
	}
	delete(u.Codes, purpose)
	s.markDirty()
	return nil
}

// deliveryDetails returns the AWS-shaped, masked destination for a code sent
// to attribute.
func deliveryDetails(u *user, attribute string) *CodeDeliveryDetailsType {
	v := u.Attributes[attribute]
	if v == "" {
		return nil
	}
	medium := "EMAIL"
	if attribute == "phone_number" {
		medium = "SMS"
	}
	return &CodeDeliveryDetailsType{Destination: maskDestination(attribute, v), DeliveryMedium: medium, AttributeName: attribute}
}

func maskDestination(attribute, v string) string {
	if attribute == "phone_number" {
		if len(v) <= 4 {
			return v
		}
		return "+" + strings.Repeat("*", len(v)-5) + v[len(v)-4:]
	}
	local, domain, ok := strings.Cut(v, "@")
	if !ok || local == "" || domain == "" {
		return "***"
	}
	host, tld, _ := strings.Cut(domain, ".")
	masked := local[:1] + "***@" + host[:1] + "***"
	if tld != "" {
		masked += "." + tld[strings.LastIndexByte(tld, '.')+1:]
	}
	return masked
}

// PendingCode is a code waiting to be used, as shown in the dashboard and
// returned by the admin codes endpoint.
type PendingCode struct {
	PoolID    string    `json:"poolId"`
	Username  string    `json:"username"`
	Purpose   string    `json:"purpose"`
	Code      string    `json:"code"`
	Attribute string    `json:"attribute,omitempty"`
	Created   time.Time `json:"created"`
	Expires   time.Time `json:"expires"`
}

// PendingCodes returns the unexpired codes for a user, or for every user in
// the pool when username is empty.
func (s *Service) PendingCodes(poolID, username string) ([]PendingCode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, err := s.lookupPool(poolID)
	if err != nil {
		return nil, err
	}
	var users []*user
	if username != "" {
		u := p.findUser(username)
		if u == nil {
			return nil, errUserNotFound
		}
		users = []*user{u}
	} else {
		for _, u := range p.Users {
			users = append(users, u)
		}
	}
	now := s.now()
	out := []PendingCode{}
	for _, u := range users {
		for purpose, c := range u.Codes {
			if now.After(c.Expires) || c.Attempts >= maxCodeAttempts {
				continue
			}
			out = append(out, PendingCode{
				PoolID: poolID, Username: u.Username, Purpose: purpose, Code: c.Code,
				Attribute: c.Attribute, Created: c.Created, Expires: c.Expires,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}
