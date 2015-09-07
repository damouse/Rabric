package rabric

import (
	"encoding/json"
	"fmt"
	"github.com/tchap/go-patricia/patricia"
	"sync"
	"time"
)

// var defaultWelcomeDetails = map[string]interface{}{
//     "roles": map[string]struct{}{
//         "broker": {},
//         "dealer": {},
//     },
// }

type node struct {
	realms                map[URI]Realm
	closing               bool
	closeLock             sync.Mutex
	sessionOpenCallbacks  []func(uint, string)
	sessionCloseCallbacks []func(uint, string)

	// "root" is a radix tree for realms and subrealms
	root *patricia.Trie
}

// NewDefaultRouter creates a very basic WAMP router.
func NewNode() Router {
	log.Println("Creating new Rabric Node")

	trie := patricia.NewTrie()
	r := &Realm{URI: "pd"}
	trie.Insert(patricia.Prefix("pd"), r)

	return &node{
		realms:                make(map[URI]Realm),
		sessionOpenCallbacks:  []func(uint, string){},
		sessionCloseCallbacks: []func(uint, string){},
		root: trie,
	}
}

func (r *node) AddSessionOpenCallback(fn func(uint, string)) {
	r.sessionOpenCallbacks = append(r.sessionOpenCallbacks, fn)
}

func (r *node) AddSessionCloseCallback(fn func(uint, string)) {
	r.sessionCloseCallbacks = append(r.sessionCloseCallbacks, fn)
}

func (r *node) Close() error {
	r.closeLock.Lock()

	if r.closing {
		r.closeLock.Unlock()
		return fmt.Errorf("already closed")
	}

	r.closing = true
	r.closeLock.Unlock()

	for _, realm := range r.realms {
		realm.Close()
	}

	// New method: walk the tree and ensure each realm is closed

	return nil
}

func (r *node) RegisterRealm(uri URI, realm Realm) error {
	// Never called, or shgould never be called
	log.Println("Asked to register a realm. Ignoring.")
	// return nil

	if _, ok := r.realms[uri]; ok {
		return RealmExistsError(uri)
	}

	realm.init()
	r.realms[uri] = realm
	return nil
}

func (r *node) Accept(client Peer) error {
	log.Println("Accepting a new connection from ", client)

	if r.closing {
		logErr(client.Send(&Abort{Reason: ErrSystemShutdown}))
		logErr(client.Close())
		return fmt.Errorf("Router is closing, no new connections are allowed")
	}

	msg, err := GetMessageTimeout(client, 5*time.Second)
	if err != nil {
		return err
	}

	// pprint the incoming details
	if b, err := json.MarshalIndent(msg, "", "  "); err != nil {
		fmt.Println("error:", err)
	} else {
		log.Printf("%s: %+v", msg.MessageType(), string(b))
	}
	// log.Printf("%s: %+v", msg.MessageType(), msg)

	hello, _ := msg.(*Hello)

	// Ensure the message is valid and well constructed
	if _, ok := msg.(*Hello); !ok {
		logErr(client.Send(&Abort{Reason: URI("wamp.error.protocol_violation")}))
		logErr(client.Close())

		return fmt.Errorf("protocol violation: expected HELLO, received %s", msg.MessageType())
	}

	// NOT IMPLEMENTED: Authenticate the user based on their provided credentials.
	// Extract their domain from said credentials. If no realm exists for that domain,
	// create it and add it to the trie.

	// New implementation: check for presence of realm
	key := patricia.Prefix(hello.Realm)

	if !r.root.MatchSubtree(key) {
		log.Println("Realm not found. Opening new realm ", hello.Realm)

		// Create the new realm
		newRealm := &Realm{URI: hello.Realm}
		newRealm.init()

		// Insert the realm into the tree
		r.root.Insert(patricia.Prefix(hello.Realm), newRealm)
		// log.Println("Created the new realm: ", newRealm)
	}

	// Extract the correct realm for this key
	// Sidenote: also an example of a cast... ish
	realm := r.root.Get(key).(*Realm)
	// log.Println("Sanity check: new realm for key: ", newRet.URI)

	// Old implementation
	// realm, _ := r.realms[hello.Realm]

	// if _, ok := r.realms[hello.Realm]; !ok {
	//     logErr(client.Send(&Abort{Reason: ErrNoSuchRealm}))
	//     logErr(client.Close())

	//     return NoSuchRealmError(hello.Realm)
	// }

	// Old implementation: the authentication must occur before fetching the realm
	welcome, err := realm.handleAuth(client, hello.Details)

	if err != nil {
		abort := &Abort{
			Reason:  ErrAuthorizationFailed, // TODO: should this be AuthenticationFailed?
			Details: map[string]interface{}{"error": err.Error()},
		}

		logErr(client.Send(abort))
		logErr(client.Close())
		return AuthenticationError(err.Error())
	}

	// No auth, no missing realms. Here is new implmementaton
	log.Println("Creating session, assigning to realm")
	// log.Println(r.root)

	// key := patricia.Prefix(hello.Realm)
	// fmt.Printf("Checking for id %q here? %v\n", key, trie.MatchSubtree(key))

	welcome.Id = NewID()

	if welcome.Details == nil {
		welcome.Details = make(map[string]interface{})
	}

	// add default details to welcome message
	for k, v := range defaultWelcomeDetails {
		if _, ok := welcome.Details[k]; !ok {
			welcome.Details[k] = v
		}
	}

	if err := client.Send(welcome); err != nil {
		return err
	}

	log.Println("Established session:", welcome.Id)

	sess := Session{Peer: client, Id: welcome.Id, kill: make(chan URI, 1)}

	// Alert any registered listeners of a new session
	for _, callback := range r.sessionOpenCallbacks {
		go callback(uint(sess.Id), string(hello.Realm))
	}

	log.Println("Details object:", welcome.Details)

	// Does this block on handleSession, then fire off .Close()?
	// Doesn't really make sense as written. Is there a blocked goroutine
	// for each session created that waits for the session to terminate?
	go func() {
		// recvSession(realm, sess, welcome.Details)
		realm.handleSession(sess, welcome.Details)
		sess.Close()

		for _, callback := range r.sessionCloseCallbacks {
			go callback(uint(sess.Id), string(hello.Realm))
		}
	}()

	return nil
}

////////////////////////////////////////
// New Content
////////////////////////////////////////

// Spin on a session, wait for messages to arrive
// func recvSession(realm *Realm, sess Session, details map[string]interface{}) {
// 	realm.handleSession(sess, details.Details)
// 	sess.Close()

// 	// for _, callback := range r.sessionCloseCallbacks {
// 	// 	go callback(uint(sess.Id), string(hello.Realm))
// 	// }
// }

func handleMessages(sess Session, c <-chan Message) (msg Message, ok bool) {
	// c := sess.Resceive()
	// TODO: what happens if the realm is closed?

	// var msg Message
	var open bool

	select {
	case msg, open = <-c:
		if !open {
			log.Println("lost session:", sess)
			return nil, false
		}

	case reason := <-sess.kill:
		logErr(sess.Send(&Goodbye{Reason: reason, Details: make(map[string]interface{})}))
		log.Printf("kill session %s: %v", sess, reason)

		// TODO: wait for client Goodbye?
		return nil, false
	}

	return msg, true
}

////////////////////////////////////////
// Code I haven't yet seen the user for
////////////////////////////////////////

// GetLocalPeer returns an internal peer connected to the specified realm.
func (r *node) GetLocalPeer(realmURI URI, details map[string]interface{}) (Peer, error) {
	peerA, peerB := localPipe()
	sess := Session{Peer: peerA, Id: NewID(), kill: make(chan URI, 1)}
	log.Println("Established internal session:", sess.Id)

	if realm, ok := r.realms[realmURI]; ok {
		// TODO: session open/close callbacks?
		if details == nil {
			details = make(map[string]interface{})
		}
		go realm.handleSession(sess, details)
	} else {
		return nil, NoSuchRealmError(realmURI)
	}
	return peerB, nil
}

func (r *node) getTestPeer() Peer {
	peerA, peerB := localPipe()
	go r.Accept(peerA)
	return peerB
}
