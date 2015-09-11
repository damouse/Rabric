'''
Autobahn examples using vanilla WAMP
'''

from os import environ
from twisted.internet import reactor
from twisted.internet.defer import inlineCallbacks

from autobahn.twisted.wamp import ApplicationSession, ApplicationRunner


class Component(ApplicationSession):

    """
    Application component that calls procedures which
    produce complex results and showing how to access those.
    """

    @inlineCallbacks
    def onJoin(self, details):
        print("session attached")

        res = yield self.call('pd.damouse/add', 2, 3)
        print 'Called add with 2 + 3 = ', res

        print 'Publishing to pd.pub'
        yield self.publish('pd.damouse/pub', 'Hello!')

        print 'Asking the other guy to die'
        res = yield self.call('pd.damouse/kill')

        self.leave()

    def onDisconnect(self):
        print("disconnected")
        reactor.stop()


if __name__ == '__main__':
    runner = ApplicationRunner(
        environ.get("AUTOBAHN_DEMO_ROUTER", "ws://127.0.0.1:8000/ws"),
        u"pd.dale",
        debug_wamp=False,  # optional; log many WAMP details
        debug=False,  # optional; log even more details
    )

    runner.run(Component)
