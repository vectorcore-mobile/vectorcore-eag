package xmpp

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/vectorcore/eag/internal/config"
	"github.com/vectorcore/eag/internal/models"
)

// XMPP namespace constants.
const (
	nsStream  = "http://etherx.jabber.org/streams"
	nsClient  = "jabber:client"
	nsTLS     = "urn:ietf:params:xml:ns:xmpp-tls"
	nsSASL    = "urn:ietf:params:xml:ns:xmpp-sasl"
	nsBind    = "urn:ietf:params:xml:ns:xmpp-bind"
	nsSession = "urn:ietf:params:xml:ns:xmpp-session"
)

// ConnStatus describes a currently connected peer for status reporting.
type ConnStatus struct {
	ID          uint      `json:"id"`
	Name        string    `json:"name"`
	Username    string    `json:"username"`
	RemoteAddr  string    `json:"remote_addr"`
	ConnectedAt time.Time `json:"connected_at"`
}

type peerSession struct {
	conn        net.Conn
	peer        models.XMPPPeer
	connectedAt time.Time
	sendMu      sync.Mutex
}

func (ps *peerSession) send(s string) error {
	ps.sendMu.Lock()
	defer ps.sendMu.Unlock()
	_, err := io.WriteString(ps.conn, s)
	return err
}

// Server is a minimal inbound XMPP server that authenticates peers and
// pushes CAP 1.2 alerts as XEP-0127 headline messages.
type Server struct {
	cfg      *config.XMPPServerConfig
	db       *gorm.DB
	tlsCfg   *tls.Config
	mu       sync.RWMutex
	sessions map[uint]*peerSession // peer ID → active session

	subsMu sync.Mutex
	subs   map[chan struct{}]struct{} // SSE subscribers notified on peer connect/disconnect
}

// Subscribe returns a channel that receives a signal on every peer connect or disconnect.
func (s *Server) Subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	s.subsMu.Lock()
	s.subs[ch] = struct{}{}
	s.subsMu.Unlock()
	return ch
}

// Unsubscribe removes a previously subscribed channel.
func (s *Server) Unsubscribe(ch chan struct{}) {
	s.subsMu.Lock()
	delete(s.subs, ch)
	s.subsMu.Unlock()
}

func (s *Server) notifyPeerChange() {
	s.subsMu.Lock()
	for ch := range s.subs {
		select {
		case ch <- struct{}{}:
		default: // drop if subscriber is not reading
		}
	}
	s.subsMu.Unlock()
}

func NewServer(cfg *config.XMPPServerConfig, db *gorm.DB) (*Server, error) {
	s := &Server{
		cfg:      cfg,
		db:       db,
		sessions: make(map[uint]*peerSession),
		subs:     make(map[chan struct{}]struct{}),
	}
	if cfg.TLS.Cert != "" && cfg.TLS.Key != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLS.Cert, cfg.TLS.Key)
		if err != nil {
			return nil, fmt.Errorf("xmpp: load TLS cert: %w", err)
		}
		s.tlsCfg = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
	}
	return s, nil
}

func (s *Server) validate() error {
	if s.cfg.C2S.STARTTLS && s.tlsCfg == nil {
		return fmt.Errorf("xmpp: c2s.starttls = true requires tls.cert and tls.key")
	}
	if s.cfg.C2STLS.Enabled && s.tlsCfg == nil {
		return fmt.Errorf("xmpp: c2s_tls.enabled = true requires tls.cert and tls.key")
	}
	return nil
}

// Start binds the configured listeners and begins accepting connections.
// It returns immediately; listeners run in background goroutines.
func (s *Server) Start(ctx context.Context) error {
	if err := s.validate(); err != nil {
		return err
	}

	var listeners []net.Listener

	if s.cfg.C2S.Enabled {
		l, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.C2S.Port))
		if err != nil {
			return fmt.Errorf("xmpp c2s: listen :%d: %w", s.cfg.C2S.Port, err)
		}
		listeners = append(listeners, l)
		slog.Info("xmpp: c2s listening", "port", s.cfg.C2S.Port, "starttls", s.cfg.C2S.STARTTLS)
		go s.acceptLoop(ctx, l, false)
	}

	if s.cfg.C2STLS.Enabled {
		l, err := tls.Listen("tcp", fmt.Sprintf(":%d", s.cfg.C2STLS.Port), s.tlsCfg)
		if err != nil {
			for _, ll := range listeners {
				ll.Close()
			}
			return fmt.Errorf("xmpp c2s_tls: listen :%d: %w", s.cfg.C2STLS.Port, err)
		}
		listeners = append(listeners, l)
		slog.Info("xmpp: c2s_tls listening", "port", s.cfg.C2STLS.Port)
		go s.acceptLoop(ctx, l, true)
	}

	go func() {
		<-ctx.Done()
		for _, l := range listeners {
			l.Close()
		}
		s.mu.Lock()
		for _, sess := range s.sessions {
			sess.conn.Close()
		}
		s.mu.Unlock()
	}()

	return nil
}

func (s *Server) acceptLoop(ctx context.Context, l net.Listener, directTLS bool) {
	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
			default:
				slog.Warn("xmpp: accept error", "error", err)
			}
			return
		}
		go s.handleConn(conn, directTLS)
	}
}

func (s *Server) handleConn(conn net.Conn, directTLS bool) {
	conn.SetDeadline(time.Now().Add(60 * time.Second)) //nolint:errcheck

	peer, finalConn, err := s.negotiate(conn, directTLS)
	if err != nil {
		slog.Warn("xmpp: negotiation failed", "remote", conn.RemoteAddr(), "error", err)
		conn.Close()
		return
	}

	finalConn.SetDeadline(time.Time{}) //nolint:errcheck

	sess := &peerSession{
		conn:        finalConn,
		peer:        *peer,
		connectedAt: time.Now(),
	}

	s.mu.Lock()
	// Disconnect existing session for this peer if one exists
	if old, ok := s.sessions[peer.ID]; ok {
		old.conn.Close()
	}
	s.sessions[peer.ID] = sess
	s.mu.Unlock()

	slog.Info("xmpp: peer connected", "peer", peer.Username, "remote", finalConn.RemoteAddr())
	s.notifyPeerChange()

	// Deliver any alerts that arrived while this peer was disconnected.
	s.SweepUnforwarded(s.db)

	defer func() {
		finalConn.Close()
		s.mu.Lock()
		if s.sessions[peer.ID] == sess {
			delete(s.sessions, peer.ID)
		}
		s.mu.Unlock()
		slog.Info("xmpp: peer disconnected", "peer", peer.Username)
		s.notifyPeerChange()
	}()

	// Read loop — detects disconnect, discards incoming stanzas (clients are receive-only).
	// Exception: session IQ (legacy RFC 3921) is responded to so clients don't hang.
	dec := xml.NewDecoder(finalConn)
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		if end, ok := tok.(xml.EndElement); ok && end.Name.Local == "stream" {
			return
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local == "iq" {
			var iq bindIQ
			if err := dec.DecodeElement(&iq, &start); err == nil && iq.SessionEl != nil {
				sess.send(fmt.Sprintf(`<iq type='result' id=%q/>`, iq.ID)) //nolint:errcheck
				continue
			}
			continue
		}
		dec.Skip() //nolint:errcheck
	}
}

// negotiate runs the full XMPP C2S stream negotiation:
//  1. Stream open
//  2. Optional STARTTLS (if c2s + starttls=true and the client requests it)
//  3. SASL PLAIN authentication
//  4. Resource bind
//
// Returns the authenticated peer and the final (possibly TLS-wrapped) connection.
func (s *Server) negotiate(conn net.Conn, directTLS bool) (*models.XMPPPeer, net.Conn, error) {
	current := conn
	tlsDone := directTLS
	var authedPeer *models.XMPPPeer

	for {
		dec := xml.NewDecoder(current)

		if err := expectStreamOpen(dec); err != nil {
			return nil, nil, fmt.Errorf("stream open: %w", err)
		}

		streamID := randHex(8)

		// ── Phase: SASL PLAIN ────────────────────────────────────────────────────
		if authedPeer == nil {
			writeStreamOpen(current, s.cfg.Domain, streamID)
			if !tlsDone && s.cfg.C2S.STARTTLS {
				fmt.Fprintf(current,
					`<stream:features><starttls xmlns=%q/><mechanisms xmlns=%q><mechanism>PLAIN</mechanism></mechanisms></stream:features>`,
					nsTLS, nsSASL)

				peer, upgradedConn, err := expectStartTLSOrSASLPlain(dec, current, s.tlsCfg, s.db)
				if err != nil {
					return nil, nil, err
				}
				if peer != nil {
					authedPeer = peer
				} else {
					current = upgradedConn
					tlsDone = true
				}
				continue // restart stream after either TLS or SASL success
			}

			fmt.Fprintf(current,
				`<stream:features><mechanisms xmlns=%q><mechanism>PLAIN</mechanism></mechanisms></stream:features>`,
				nsSASL)

			peer, err := expectSASLPlain(dec, current, s.db)
			if err != nil {
				return nil, nil, fmt.Errorf("sasl: %w", err)
			}
			authedPeer = peer
			continue // restart stream
		}

		// ── Phase: resource bind (post-auth) ────────────────────────────────────
		writeStreamOpen(current, s.cfg.Domain, streamID)
		fmt.Fprintf(current,
			`<stream:features><bind xmlns=%q/><session xmlns=%q/></stream:features>`,
			nsBind, nsSession)

		if err := expectBind(dec, current, authedPeer, s.cfg.Domain); err != nil {
			return nil, nil, fmt.Errorf("bind: %w", err)
		}

		return authedPeer, current, nil
	}
}

// ── Stream helpers ───────────────────────────────────────────────────────────

func writeStreamOpen(w io.Writer, domain, id string) {
	fmt.Fprintf(w,
		`<?xml version='1.0'?>`+
			`<stream:stream from=%q id=%q xmlns=%q xmlns:stream=%q version='1.0'>`,
		domain, id, nsClient, nsStream)
}

// expectStreamOpen skips the XML declaration and reads the <stream:stream> opening element.
func expectStreamOpen(dec *xml.Decoder) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.ProcInst, xml.CharData, xml.Comment:
			continue
		case xml.StartElement:
			if t.Name.Local == "stream" {
				return nil
			}
			return fmt.Errorf("expected stream:stream, got <%s>", t.Name.Local)
		}
	}
}

// expectStartTLSOrSASLPlain handles the first client action after features are
// advertised on a STARTTLS-capable c2s listener. The client may either upgrade
// the connection with STARTTLS or authenticate immediately over plain TCP.
func expectStartTLSOrSASLPlain(dec *xml.Decoder, conn net.Conn, tlsCfg *tls.Config, db *gorm.DB) (*models.XMPPPeer, net.Conn, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Space == nsTLS && start.Name.Local == "starttls" {
			dec.Skip() //nolint:errcheck
			fmt.Fprintf(conn, `<proceed xmlns=%q/>`, nsTLS)
			tlsConn := tls.Server(conn, tlsCfg)
			if err := tlsConn.Handshake(); err != nil {
				return nil, nil, fmt.Errorf("tls handshake: %w", err)
			}
			return nil, tlsConn, nil
		}
		if start.Name.Space == nsSASL && start.Name.Local == "auth" {
			peer, err := decodeSASLPlain(dec, conn, db, start)
			if err != nil {
				return nil, nil, fmt.Errorf("sasl: %w", err)
			}
			return peer, conn, nil
		}
		return nil, nil, fmt.Errorf("expected <starttls> or <auth>, got <%s>", start.Name.Local)
	}
}

type saslAuthStanza struct {
	XMLName   xml.Name `xml:"auth"`
	Mechanism string   `xml:"mechanism,attr"`
	Text      string   `xml:",chardata"`
}

// expectSASLPlain reads and validates a SASL PLAIN <auth> stanza.
// Returns the authenticated XMPPPeer on success.
func expectSASLPlain(dec *xml.Decoder, w io.Writer, db *gorm.DB) (*models.XMPPPeer, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Space != nsSASL || start.Name.Local != "auth" {
			dec.Skip() //nolint:errcheck
			continue
		}
		return decodeSASLPlain(dec, w, db, start)
	}
}

func decodeSASLPlain(dec *xml.Decoder, w io.Writer, db *gorm.DB, start xml.StartElement) (*models.XMPPPeer, error) {
	var auth saslAuthStanza
	if err := dec.DecodeElement(&auth, &start); err != nil {
		return nil, err
	}
	if auth.Mechanism != "PLAIN" {
		fmt.Fprintf(w, `<failure xmlns=%q><invalid-mechanism/></failure>`, nsSASL)
		return nil, fmt.Errorf("unsupported mechanism: %s", auth.Mechanism)
	}

	// SASL PLAIN payload: base64(authzid \0 authcid \0 passwd)
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(auth.Text))
	if err != nil {
		fmt.Fprintf(w, `<failure xmlns=%q><incorrect-encoding/></failure>`, nsSASL)
		return nil, fmt.Errorf("base64 decode: %w", err)
	}

	parts := strings.SplitN(string(raw), "\x00", 3)
	if len(parts) != 3 {
		fmt.Fprintf(w, `<failure xmlns=%q><incorrect-encoding/></failure>`, nsSASL)
		return nil, fmt.Errorf("invalid SASL PLAIN format")
	}
	username := parts[1]
	password := parts[2]

	var peer models.XMPPPeer
	if err := db.Where("username = ? AND enabled = ?", username, true).First(&peer).Error; err != nil {
		fmt.Fprintf(w, `<failure xmlns=%q><not-authorized/></failure>`, nsSASL)
		return nil, fmt.Errorf("peer not found: %s", username)
	}
	if peer.Password != password {
		fmt.Fprintf(w, `<failure xmlns=%q><not-authorized/></failure>`, nsSASL)
		return nil, fmt.Errorf("bad password for peer: %s", username)
	}

	fmt.Fprintf(w, `<success xmlns=%q/>`, nsSASL)
	return &peer, nil
}

type bindIQ struct {
	XMLName   xml.Name   `xml:"iq"`
	Type      string     `xml:"type,attr"`
	ID        string     `xml:"id,attr"`
	BindEl    *bindEl    `xml:"urn:ietf:params:xml:ns:xmpp-bind bind"`
	SessionEl *sessionEl `xml:"urn:ietf:params:xml:ns:xmpp-session session"`
}

type bindEl struct {
	Resource string `xml:"resource"`
}

type sessionEl struct{}

// expectBind handles the resource-bind IQ exchange and returns immediately once
// the bind result has been sent. Session IQ handling (legacy) is deferred to
// the main read loop so we don't block waiting for a stanza the client may never send.
func expectBind(dec *xml.Decoder, w io.Writer, peer *models.XMPPPeer, domain string) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local != "iq" {
			dec.Skip() //nolint:errcheck
			continue
		}

		var iq bindIQ
		if err := dec.DecodeElement(&iq, &start); err != nil {
			return err
		}

		if iq.BindEl != nil {
			resource := iq.BindEl.Resource
			if resource == "" {
				resource = "eag"
			}
			jid := fmt.Sprintf("%s@%s/%s", peer.Username, domain, resource)
			fmt.Fprintf(w,
				`<iq type='result' id=%q><bind xmlns=%q><jid>%s</jid></bind></iq>`,
				iq.ID, nsBind, xmlEscape(jid))
			return nil // session is now established; don't wait for optional session IQ
		}
	}
}

// ── Alert broadcasting ────────────────────────────────────────────────────────

// Broadcast sends a CAP alert to all connected peers whose filters match.
// Cancel alerts are never forwarded.
func (s *Server) Broadcast(alert *models.Alert, db *gorm.DB) {
	if alert.MsgType == "Cancel" {
		return
	}

	s.mu.RLock()
	sessions := make([]*peerSession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.RUnlock()

	forwarded := false
	for _, sess := range sessions {
		if !sess.peer.Enabled {
			continue
		}
		if !matchesFilters(alert, sess.peer) {
			continue
		}
		toJID := fmt.Sprintf("%s@%s", sess.peer.Username, s.cfg.Domain)
		stanza := buildCAPStanza(toJID, alert)
		if err := sess.send(stanza); err != nil {
			slog.Error("xmpp: send failed", "peer", sess.peer.Username, "alert", alert.ID, "error", err)
			continue
		}
		forwarded = true
	}

	if forwarded {
		db.Model(alert).Update("forwarded", true) //nolint:errcheck
		alert.Forwarded = true
	}
}

// SweepUnforwarded re-evaluates all pending alerts against connected peers.
// Called when a peer connects or is enabled.
func (s *Server) SweepUnforwarded(db *gorm.DB) {
	var alerts []models.Alert
	if err := db.Where("forwarded = ? AND msg_type != ? AND deleted_at IS NULL", false, "Cancel").
		Find(&alerts).Error; err != nil || len(alerts) == 0 {
		return
	}
	go func() {
		for i := range alerts {
			s.Broadcast(&alerts[i], db)
		}
	}()
}

// Status returns info about currently connected peers.
func (s *Server) Status() []ConnStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ConnStatus, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, ConnStatus{
			ID:          sess.peer.ID,
			Name:        sess.peer.Name,
			Username:    sess.peer.Username,
			RemoteAddr:  sess.conn.RemoteAddr().String(),
			ConnectedAt: sess.connectedAt,
		})
	}
	return out
}

// ── Filter evaluation ─────────────────────────────────────────────────────────

func matchesFilters(alert *models.Alert, peer models.XMPPPeer) bool {
	if !checkList(peer.FilterSeverity, func(v string) bool { return alert.Severity == v }) {
		return false
	}
	if !checkList(peer.FilterEvent, func(v string) bool { return strings.Contains(alert.Event, v) }) {
		return false
	}
	if !checkList(peer.FilterArea, func(v string) bool { return strings.Contains(alert.AreaDesc, v) }) {
		return false
	}
	if !checkList(peer.FilterStatus, func(v string) bool { return alert.Status == v }) {
		return false
	}
	return true
}

func checkList(jsonList string, pred func(string) bool) bool {
	if jsonList == "" || jsonList == "null" || jsonList == "[]" {
		return true
	}
	var vals []string
	if err := json.Unmarshal([]byte(jsonList), &vals); err != nil || len(vals) == 0 {
		return true
	}
	for _, v := range vals {
		if pred(v) {
			return true
		}
	}
	return false
}

// ── CAP stanza builders ───────────────────────────────────────────────────────

func buildCAPStanza(toJID string, a *models.Alert) string {
	payload := a.RawCAP
	if payload == "" || !strings.HasPrefix(strings.TrimSpace(payload), "<") {
		// RawCAP absent or not XML — fall back to building from model fields
		payload = buildCAPXML(a)
	}
	return fmt.Sprintf(`<message to=%q type="headline">%s</message>`,
		xmlEscape(toJID), payload)
}

func buildCAPXML(a *models.Alert) string {
	var b strings.Builder
	b.WriteString(`<alert xmlns="urn:oasis:names:tc:emergency:cap:1.2">`)
	b.WriteString(xmlTag("identifier", a.ID))
	b.WriteString(xmlTag("sender", a.Sender))
	b.WriteString(xmlTag("sent", capDateTime(a.Sent)))
	b.WriteString(xmlTag("status", a.Status))
	b.WriteString(xmlTag("msgType", a.MsgType))
	b.WriteString(xmlTag("scope", a.Scope))
	if a.References != "" {
		b.WriteString(xmlTag("references", a.References))
	}
	b.WriteString("<info>")
	b.WriteString(xmlTag("language", "en-US"))
	// category is required by the CAP 1.2 schema (<info> sequence: category+,
	// event, ...) — this fallback path has no source category to draw on, so
	// "Other" is the safe default (same fallback used elsewhere for
	// unclassified alerts).
	b.WriteString(xmlTag("category", "Other"))
	b.WriteString(xmlTag("event", a.Event))
	b.WriteString(xmlTag("urgency", a.Urgency))
	b.WriteString(xmlTag("severity", a.Severity))
	b.WriteString(xmlTag("certainty", a.Certainty))
	if !a.Effective.IsZero() {
		b.WriteString(xmlTag("effective", capDateTime(a.Effective)))
	}
	if !a.Onset.IsZero() {
		b.WriteString(xmlTag("onset", capDateTime(a.Onset)))
	}
	if !a.Expires.IsZero() {
		b.WriteString(xmlTag("expires", capDateTime(a.Expires)))
	}
	if a.Headline != "" {
		b.WriteString(xmlTag("headline", a.Headline))
	}
	if a.Description != "" {
		b.WriteString(xmlTag("description", a.Description))
	}
	if a.AreaDesc != "" {
		b.WriteString("<area>")
		b.WriteString(xmlTag("areaDesc", a.AreaDesc))
		b.WriteString("</area>")
	}
	b.WriteString("</info>")
	b.WriteString("</alert>")
	return b.String()
}

func xmlTag(name, value string) string {
	return fmt.Sprintf("<%s>%s</%s>", name, xmlEscape(value), name)
}

// capDateTime formats a time as CAP 1.2's required dateTime form
// "YYYY-MM-DDThh:mm:ssXzh:zm". The spec explicitly prohibits the "Z"
// designator that Go's time.RFC3339 produces for UTC — "Alphabetic timezone
// designators such as 'Z' MUST NOT be used. The timezone for UTC MUST be
// represented as '-00:00'" (CAP-v1.2-os §3.3.2) — so UTC times need the
// "+00:00" Go's numeric offset format would otherwise emit rewritten to
// "-00:00".
func capDateTime(t time.Time) string {
	s := t.UTC().Format("2006-01-02T15:04:05-07:00")
	if strings.HasSuffix(s, "+00:00") {
		s = strings.TrimSuffix(s, "+00:00") + "-00:00"
	}
	return s
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// ValidateTLSMode kept for any callers that may reference it — no longer used internally.
func ValidateTLSMode(mode string) error {
	switch mode {
	case "none", "starttls", "direct":
		return nil
	default:
		return fmt.Errorf("tls_mode must be one of: none, starttls, direct")
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b) //nolint:errcheck
	return fmt.Sprintf("%x", b)
}
