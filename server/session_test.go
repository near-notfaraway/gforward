package server

import (
	"errors"
	"testing"

	"github.com/panjf2000/gnet/v2"
	. "github.com/smartystreets/goconvey/convey"
)

func TestNewSessionTable(t *testing.T) {
	Convey("newSessionTable should initialize both route maps", t, func() {
		table := newSessionTable()

		So(table.byClient, ShouldNotBeNil)
		So(table.byDest, ShouldNotBeNil)
		So(len(table.byClient), ShouldEqual, 0)
		So(len(table.byDest), ShouldEqual, 0)
	})
}

func TestSessionTableAdmitUpstream(t *testing.T) {
	Convey("admitUpstream should decide the upstream action under lock", t, func() {
		clientConn := &stubConn{}
		table := newSessionTable()

		Convey("No existing session should create a dialing session and request dial", func() {
			act := table.admitUpstream(clientConn, "example.com:80", []byte("first"))

			So(act.action, ShouldEqual, upstreamActionDial)
			So(act.dest, ShouldEqual, "example.com:80")
			So(act.oldDestConn, ShouldBeNil)
			So(act.session, ShouldNotBeNil)
			So(act.session.dialing, ShouldBeTrue)
			So(act.session.pending, ShouldResemble, [][]byte{[]byte("first")})
			So(table.byClient[clientConn], ShouldEqual, act.session)
		})

		Convey("Same destination while dialing should queue the payload", func() {
			table.byClient[clientConn] = &session{
				dest:    "example.com:80",
				dialing: true,
				pending: [][]byte{[]byte("first")},
			}

			act := table.admitUpstream(clientConn, "example.com:80", []byte("second"))

			So(act.action, ShouldEqual, upstreamActionQueued)
			So(table.byClient[clientConn].pending, ShouldResemble, [][]byte{[]byte("first"), []byte("second")})
		})

		Convey("Same destination already ready should forward to the destination conn", func() {
			destConn := &stubConn{}
			table.byClient[clientConn] = &session{
				dest:     "example.com:80",
				destConn: destConn,
			}

			act := table.admitUpstream(clientConn, "example.com:80", []byte("payload"))

			So(act.action, ShouldEqual, upstreamActionForward)
			So(act.destConn, ShouldEqual, destConn)
			So(act.payload, ShouldResemble, []byte("payload"))
		})

		Convey("Switching destination should tear down the old route and dial anew", func() {
			oldDestConn := &stubConn{}
			table.byClient[clientConn] = &session{
				dest:     "old.com:80",
				destConn: oldDestConn,
			}
			table.byDest[oldDestConn] = clientConn

			act := table.admitUpstream(clientConn, "new.com:80", []byte("payload"))

			So(act.action, ShouldEqual, upstreamActionDial)
			So(act.dest, ShouldEqual, "new.com:80")
			So(act.oldDestConn, ShouldEqual, oldDestConn)
			So(act.session.dialing, ShouldBeTrue)
			So(table.byClient[clientConn], ShouldEqual, act.session)
			_, oldReverseExists := table.byDest[oldDestConn]
			So(oldReverseExists, ShouldBeFalse)
		})

		Convey("Switching from a still-dialing session should not report an old dest conn", func() {
			table.byClient[clientConn] = &session{
				dest:    "old.com:80",
				dialing: true,
				pending: [][]byte{[]byte("stale")},
			}

			act := table.admitUpstream(clientConn, "new.com:80", []byte("payload"))

			So(act.action, ShouldEqual, upstreamActionDial)
			So(act.oldDestConn, ShouldBeNil)
			So(act.session.pending, ShouldResemble, [][]byte{[]byte("payload")})
		})
	})
}

func TestSessionTableCompleteDial(t *testing.T) {
	Convey("completeDial should validate the session before flipping to ready", t, func() {
		clientConn := &stubConn{}
		destConn := &stubConn{}
		table := newSessionTable()

		Convey("Missing client session should report not matched", func() {
			sess := &session{dest: "example.com:80", dialing: true}

			pending, matched := table.completeDial(clientConn, sess, destConn, nil)

			So(matched, ShouldBeFalse)
			So(pending, ShouldBeNil)
		})

		Convey("Session pointer mismatch should report not matched", func() {
			current := &session{dest: "example.com:80", dialing: true}
			stale := &session{dest: "example.com:80", dialing: true}
			table.byClient[clientConn] = current

			pending, matched := table.completeDial(clientConn, stale, destConn, nil)

			So(matched, ShouldBeFalse)
			So(pending, ShouldBeNil)
			So(table.byClient[clientConn], ShouldEqual, current)
		})

		Convey("Matched dial error should remove the session and return no pending", func() {
			sess := &session{dest: "example.com:80", dialing: true, pending: [][]byte{[]byte("x")}}
			table.byClient[clientConn] = sess

			pending, matched := table.completeDial(clientConn, sess, nil, errors.New("dial failed"))

			So(matched, ShouldBeTrue)
			So(pending, ShouldBeNil)
			_, exists := table.byClient[clientConn]
			So(exists, ShouldBeFalse)
		})

		Convey("Matched success should mark ready and return pending payloads", func() {
			sess := &session{
				dest:    "example.com:80",
				dialing: true,
				pending: [][]byte{[]byte("a"), []byte("b")},
			}
			table.byClient[clientConn] = sess

			pending, matched := table.completeDial(clientConn, sess, destConn, nil)

			So(matched, ShouldBeTrue)
			So(pending, ShouldResemble, [][]byte{[]byte("a"), []byte("b")})
			So(sess.destConn, ShouldEqual, destConn)
			So(sess.dialing, ShouldBeFalse)
			So(sess.pending, ShouldBeNil)
			So(table.byDest[destConn], ShouldEqual, clientConn)
		})
	})
}

func TestSessionTableCloseClient(t *testing.T) {
	Convey("closeClient should remove the session and surface the dest conn to close", t, func() {
		clientConn := &stubConn{}
		table := newSessionTable()

		Convey("Unknown client should return no dest conn", func() {
			destConn := table.closeClient(clientConn)

			So(destConn, ShouldBeNil)
		})

		Convey("Ready session should remove both routes and return the dest conn", func() {
			connToDest := &stubConn{}
			table.byClient[clientConn] = &session{dest: "example.com:80", destConn: connToDest}
			table.byDest[connToDest] = clientConn

			destConn := table.closeClient(clientConn)

			So(destConn, ShouldEqual, connToDest)
			_, clientExists := table.byClient[clientConn]
			_, destExists := table.byDest[connToDest]
			So(clientExists, ShouldBeFalse)
			So(destExists, ShouldBeFalse)
		})

		Convey("Dialing session without dest conn should remove client route only", func() {
			table.byClient[clientConn] = &session{dest: "example.com:80", dialing: true}

			destConn := table.closeClient(clientConn)

			So(destConn, ShouldBeNil)
			_, clientExists := table.byClient[clientConn]
			So(clientExists, ShouldBeFalse)
		})
	})
}

func TestSessionTableClientByDest(t *testing.T) {
	Convey("clientByDest should look up the client conn by dest conn", t, func() {
		clientConn := &stubConn{}
		destConn := &stubConn{}
		table := newSessionTable()

		Convey("Known dest conn should return its client conn", func() {
			table.byDest[destConn] = clientConn

			got, ok := table.clientByDest(destConn)

			So(ok, ShouldBeTrue)
			So(got, ShouldEqual, clientConn)
		})

		Convey("Unknown dest conn should report miss", func() {
			var unknown gnet.Conn = &stubConn{}

			got, ok := table.clientByDest(unknown)

			So(ok, ShouldBeFalse)
			So(got, ShouldBeNil)
		})
	})
}
