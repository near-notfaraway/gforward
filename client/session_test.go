package client

import (
	"testing"

	"github.com/panjf2000/gnet/v2"
	. "github.com/smartystreets/goconvey/convey"
)

func TestNewSessionTable(t *testing.T) {
	Convey("newSessionTable should initialize both route maps", t, func() {
		table := newSessionTable()

		So(table.byUser, ShouldNotBeNil)
		So(table.byServer, ShouldNotBeNil)
		So(len(table.byUser), ShouldEqual, 0)
		So(len(table.byServer), ShouldEqual, 0)
	})
}

func TestSessionTableAdmitUpstream(t *testing.T) {
	Convey("admitUpstream should decide the upstream action under lock", t, func() {
		userConn := &trackingConn{}
		table := newSessionTable()

		Convey("No session should register a dialing session and request a dial", func() {
			act := table.admitUpstream(userConn, "example.com:80", []byte("first"))

			So(act.action, ShouldEqual, upstreamActionDial)
			So(act.session, ShouldNotBeNil)
			So(act.session.dialing, ShouldBeTrue)
			So(act.session.dest, ShouldEqual, "example.com:80")
			So(act.session.pending, ShouldResemble, [][]byte{[]byte("first")})
			So(table.byUser[userConn], ShouldEqual, act.session)
		})

		Convey("An empty cachedDest should register a per-request session", func() {
			act := table.admitUpstream(userConn, "", []byte("first"))

			So(act.action, ShouldEqual, upstreamActionDial)
			So(table.byUser[userConn].dest, ShouldEqual, "")
		})

		Convey("A dialing session should queue the packet", func() {
			table.byUser[userConn] = &session{dialing: true, pending: [][]byte{[]byte("first")}}

			act := table.admitUpstream(userConn, "example.com:80", []byte("second"))

			So(act.action, ShouldEqual, upstreamActionQueued)
			So(table.byUser[userConn].pending, ShouldResemble, [][]byte{[]byte("first"), []byte("second")})
		})

		Convey("A ready session should forward to the server conn", func() {
			serverConn := &trackingConn{}
			sess := &session{serverConn: serverConn}
			table.byUser[userConn] = sess

			act := table.admitUpstream(userConn, "example.com:80", []byte("payload"))

			So(act.action, ShouldEqual, upstreamActionForward)
			So(act.serverConn, ShouldEqual, serverConn)
			So(act.packet, ShouldResemble, []byte("payload"))
			So(act.session, ShouldEqual, sess)
		})
	})
}

func TestSessionTableGetDest(t *testing.T) {
	Convey("getDest should return the cached destination only when present", t, func() {
		userConn := &trackingConn{}
		table := newSessionTable()

		Convey("Unknown user conn should miss", func() {
			dest, ok := table.getDest(userConn)

			So(ok, ShouldBeFalse)
			So(dest, ShouldEqual, "")
		})

		Convey("A cached destination should be returned", func() {
			table.byUser[userConn] = &session{dest: "example.com:80"}

			dest, ok := table.getDest(userConn)

			So(ok, ShouldBeTrue)
			So(dest, ShouldEqual, "example.com:80")
		})

		Convey("A per-request session with an empty dest should miss", func() {
			table.byUser[userConn] = &session{dest: ""}

			_, ok := table.getDest(userConn)

			So(ok, ShouldBeFalse)
		})
	})
}

func TestSessionTableCompleteDial(t *testing.T) {
	Convey("completeDial should register the reverse route and hand over pending", t, func() {
		userConn := &trackingConn{}
		serverConn := &trackingConn{}
		table := newSessionTable()

		Convey("Missing user session should report not matched", func() {
			sess := &session{dialing: true}

			pending, matched := table.completeDial(userConn, sess, serverConn)

			So(matched, ShouldBeFalse)
			So(pending, ShouldBeNil)
		})

		Convey("A session pointer mismatch should report not matched", func() {
			current := &session{dialing: true}
			stale := &session{dialing: true}
			table.byUser[userConn] = current

			pending, matched := table.completeDial(userConn, stale, serverConn)

			So(matched, ShouldBeFalse)
			So(pending, ShouldBeNil)
			So(table.byUser[userConn], ShouldEqual, current)
		})

		Convey("A matched session should flip ready and return pending", func() {
			sess := &session{dialing: true, pending: [][]byte{[]byte("a"), []byte("b")}}
			table.byUser[userConn] = sess

			pending, matched := table.completeDial(userConn, sess, serverConn)

			So(matched, ShouldBeTrue)
			So(pending, ShouldResemble, [][]byte{[]byte("a"), []byte("b")})
			So(sess.serverConn, ShouldEqual, serverConn)
			So(sess.dialing, ShouldBeFalse)
			So(sess.pending, ShouldBeNil)
			So(table.byServer[serverConn], ShouldEqual, userConn)
		})
	})
}

func TestSessionTableAbortDial(t *testing.T) {
	Convey("abortDial should remove the dialing session only when it still matches", t, func() {
		userConn := &trackingConn{}
		table := newSessionTable()

		Convey("Missing user session should report not matched", func() {
			matched := table.abortDial(userConn, &session{dialing: true})

			So(matched, ShouldBeFalse)
		})

		Convey("A session pointer mismatch should keep the current session", func() {
			current := &session{dialing: true}
			table.byUser[userConn] = current

			matched := table.abortDial(userConn, &session{dialing: true})

			So(matched, ShouldBeFalse)
			So(table.byUser[userConn], ShouldEqual, current)
		})

		Convey("A matched session should be removed", func() {
			sess := &session{dialing: true}
			table.byUser[userConn] = sess

			matched := table.abortDial(userConn, sess)

			So(matched, ShouldBeTrue)
			_, exists := table.byUser[userConn]
			So(exists, ShouldBeFalse)
		})
	})
}

func TestSessionTablePurgeByUser(t *testing.T) {
	Convey("purgeByUser should remove the session and surface the server conn to close", t, func() {
		userConn := &trackingConn{}
		table := newSessionTable()

		Convey("Unknown user should return no server conn", func() {
			So(table.purgeByUser(userConn), ShouldBeNil)
		})

		Convey("A ready session should remove both routes and return the server conn", func() {
			serverConn := &trackingConn{}
			table.byUser[userConn] = &session{serverConn: serverConn}
			table.byServer[serverConn] = userConn

			got := table.purgeByUser(userConn)

			So(got, ShouldEqual, serverConn)
			_, userExists := table.byUser[userConn]
			_, serverExists := table.byServer[serverConn]
			So(userExists, ShouldBeFalse)
			So(serverExists, ShouldBeFalse)
		})

		Convey("A dialing session without a server conn should remove the user route only", func() {
			table.byUser[userConn] = &session{dialing: true}

			So(table.purgeByUser(userConn), ShouldBeNil)
			_, userExists := table.byUser[userConn]
			So(userExists, ShouldBeFalse)
		})
	})
}

func TestSessionTablePurgeByServer(t *testing.T) {
	Convey("purgeByServer should clean the matching routes only", t, func() {
		userConn := &trackingConn{}
		table := newSessionTable()

		Convey("Unknown server conn should return no user conn", func() {
			So(table.purgeByServer(&trackingConn{}), ShouldBeNil)
		})

		Convey("Closing the current server conn should remove both routes", func() {
			serverConn := &trackingConn{}
			table.byUser[userConn] = &session{serverConn: serverConn}
			table.byServer[serverConn] = userConn

			got := table.purgeByServer(serverConn)

			So(got, ShouldEqual, userConn)
			_, userExists := table.byUser[userConn]
			_, serverExists := table.byServer[serverConn]
			So(userExists, ShouldBeFalse)
			So(serverExists, ShouldBeFalse)
		})

		Convey("A stale server conn should not remove a re-pointed user route", func() {
			oldServerConn := &trackingConn{}
			currentServerConn := &trackingConn{}
			table.byUser[userConn] = &session{serverConn: currentServerConn}
			table.byServer[oldServerConn] = userConn
			table.byServer[currentServerConn] = userConn

			got := table.purgeByServer(oldServerConn)

			So(got, ShouldBeNil)
			So(table.byUser[userConn].serverConn, ShouldEqual, currentServerConn)
			_, oldExists := table.byServer[oldServerConn]
			So(oldExists, ShouldBeFalse)
		})
	})
}

func TestSessionTableGetByServer(t *testing.T) {
	Convey("getByServer should look up the user conn by server conn", t, func() {
		userConn := &trackingConn{}
		serverConn := &trackingConn{}
		table := newSessionTable()

		Convey("A known server conn should return its user conn", func() {
			table.byServer[serverConn] = userConn

			got, ok := table.getByServer(serverConn)

			So(ok, ShouldBeTrue)
			So(got, ShouldEqual, userConn)
		})

		Convey("An unknown server conn should miss", func() {
			var unknown gnet.Conn = &trackingConn{}

			got, ok := table.getByServer(unknown)

			So(ok, ShouldBeFalse)
			So(got, ShouldBeNil)
		})
	})
}
