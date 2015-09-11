package rabric

import (
	"fmt"
	"sync"
	"time"
)

type node struct {
	closing               bool
	closeLock             sync.Mutex
	sessionOpenCallbacks  []func(uint, string)
	sessionCloseCallbacks []func(uint, string)
	realms                map[URI]Realm
	sessionPdid           map[string]string
	nodes                 map[string]Session
	forwarding            map[string]Session
	permissions           map[string]string
    agent                 *Client
}

// NewDefaultRouter creates a very basic WAMP router.
func NewNode() Router {
    node := &node{
        realms:                make(map[URI]Realm),
        sessionOpenCallbacks:  []func(uint, string){},
        sessionCloseCallbacks: []func(uint, string){},
    }

    // Provisioning: this router needs a name
    // Unhandled case: what to do with routers that start with nothing?
    // They still have to create a peer (self) and ask for an identity

    // Here we assume *one* router as root and a default pd namespace
    realm := Realm{URI: "pd"}
    realm.init()

    node.realms["pd"] = realm

    // peer, ok := node.GetLocalPeer("pd", nil)

    // if peer, ok := node.GetLocalPeer("pd", nil); ok != nil {
    //     log.Println("Unable to create local session: ", ok)
    // } else {
    //     node.agent = peer
    // }

    node.agent = node.localClient("pd")

    h := func(args []interface{}, kwargs map[string]interface{})  {
        log.Println("Got a pub on the local session!")
    }

    // NOTE: this works, but looks like an error with the extraction and parsing code
    // when published on this endpoint.
    node.agent.Subscribe("pd/hello", h)

    // What does a provisioning process look like? Where does the router get its name?
    // what is OUR name?

	return node
}

func (n *node) localClient(s string) *Client {
    p := n.getTestPeer()

    // p, _ := n.GetLocalPeer(URI(s), nil)

    client := NewClient(p)
    client.ReceiveTimeout = 100 * time.Millisecond
    _, err := client.JoinRealm(s, nil)

    log.Println(err)

    return client
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

	return nil
}

// Shouldn't be called anymore
func (r *node) RegisterRealm(uri URI, realm Realm) error {
	return nil
}

func (r *node) Accept(client Peer) error {

	// Dont accept new sessions if the node is going down
	if r.closing {
		logErr(client.Send(&Abort{Reason: ErrSystemShutdown}))
		logErr(client.Close())
		return fmt.Errorf("Router is closing, no new connections are allowed")
	}

	msg, err := GetMessageTimeout(client, 5*time.Second)
	if err != nil {
		return err
	}

	// pprint the incoming details. Still needed?
	// if b, err := json.MarshalIndent(msg, "", "  "); err != nil {
	// 	fmt.Println("error:", err)
	// } else {
	// 	log.Printf("%s: %+v", msg.MessageType(), string(b))
	// }
	// log.Printf("%s: %+v", msg.MessageType(), msg)

	hello, _ := msg.(*Hello)

	// Ensure the message is valid and well constructed
	if _, ok := msg.(*Hello); !ok {
		logErr(client.Send(&Abort{Reason: URI("wamp.error.protocol_violation")}))
		logErr(client.Close())

		return fmt.Errorf("protocol violation: expected HELLO, received %s", msg.MessageType())
	}

	// Old implementation
	realm, exists := r.realms[hello.Realm]

	if !exists {
		log.Println("Domain not found, creating new domain for ", hello.Realm)

		realm = Realm{URI: hello.Realm}
		realm.init()
		r.realms[hello.Realm] = realm
	}

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

	log.Println("Established session: ", hello.Realm)
	// log.Println("Established session: ", welcome.Id)

	sess := Session{Peer: client, Id: welcome.Id, pdid: hello.Realm, kill: make(chan URI, 1)}

	// Meta level start events
	for _, callback := range r.sessionOpenCallbacks {
		go callback(uint(sess.Id), string(hello.Realm))
	}

	// Start listening on the session
	go func() {
		recvSession(&realm, sess, welcome.Details)
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

// Spin on a session, wait for messages to arrive. Method does not return
// until session closes
// Can you pass in the realm here? The messages may not be intended for this domain!
func recvSession(realm *Realm, sess Session, details map[string]interface{}) {
	c := sess.Receive()

	for {
		msg, ok := sessionRecieve(sess, c)

		if !ok {
			log.Println("Session closing with status: ", ok)
			break
		}

        // NOTE: there is a serious shortcoming here: How do we deal with WAMP messages with an
        // implicit destination? Many of them refer to sessions, but do we want to store the session 
        // IDs with the ultimate PDID target, or just change the protocol?

        if uri, ok := destination(&msg); ok == nil {
            // log.Println("Domain extracted as ", uri)

            // Ensure the construction of the message is valid, extract the endpoint, domain, and action
            domain, action, err := breakdownEndpoint(string(uri))

            // Return a WAMP error to the user indicating a poorly constructed endpoint
            if err != nil {
                log.Println("Misconstructed endpoint. Dont know what to do now!")
                continue
            }

            log.Printf("Extracted: %s %s \n", domain, action)
            log.Println("Current realm: ", realm)
 
            // Permissions
            // Is downward action? allow
            // Check permissions cache: if found, allow
            // Check with bouncer

            // Delivery
            // Is target a tenant? 
            // Is target in forwarding tables?
            // Ask map for next hop


        } else {
            log.Println("Unable to determine destination from message.")
        }

        // if asking a realm to handle the message, assume this is for local delivery
		realm.handleMessage(msg, sess, details)
	}
}

// receive messages from a session
func sessionRecieve(sess Session, c <-chan Message) (msg Message, ok bool) {
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

////////////////////////////////////////
// Very new code
////////////////////////////////////////

// Handle a new message
func (n *node) Handle(msg *Message) {

}

// Return true or false based on the message and the session which sent the messate
func (n *node) Permitted(msg *Message, sess *Session) bool {
    return true
}

// returns the pdid of the next hop on the path for the given message
func (n *node) Route(msg *Message) string {
    return ""
}