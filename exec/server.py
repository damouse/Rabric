'''
There is no distinction between client and server here. Names used here as 
files to keep them seperate. 
'''


from twisted.internet.defer import inlineCallbacks, returnValue, Deferred

from autobahn.twisted.wamp import ApplicationSession, ApplicationRunner
from autobahn.wamp.types import RegisterOptions, SubscribeOptions
from pdtools.lib import cxbr

HOST = "ws://127.0.0.1:8000/ws"
# HOST = "ws://paradrop.io:9080/ws"
# PORT = 9080


class Server(cxbr.BaseSession):

    @inlineCallbacks
    def onJoin(self, details):
        yield cxbr.BaseSession.onJoin(self, details)

        yield self.register(self.method, 'call')
        yield self.subscribe(self.subscription, 'pd', 'sub')

    def subscription(self, pdid):
        print 'Server.subscription called from: ' + pdid

    def method(self, pdid, blah):
        print 'Server.method called from: ' + pdid + ' with argument: ' + str(blah)
        return 'Server method receipt'


def main():
    Server.start(HOST, 'pd', start_reactor=True)


if __name__ == '__main__':
    main()
