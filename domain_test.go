package rabric

import (
    "testing"
    . "github.com/smartystreets/goconvey/convey"
)

func TestValidDomain(t *testing.T) {
    Convey("Valid endpoints", t, func() {
        Convey("Don't need to have periods", func() {
            So(validEndpoint("pd"), ShouldBeTrue)
        })

        Convey("Can have a single subdomain", func() {
            So(validEndpoint("pd.damouse"), ShouldBeTrue)
        })

        Convey("Can have an arbitrary number of subdomains", func() {
            So(validEndpoint("pd.damouse.a.b.c"), ShouldBeTrue)
        })
    })

    Convey("Invalid endpoints", t, func() {
        Convey("Cannot end in an period", func() {
            So(validEndpoint("pd."), ShouldBeFalse)
        })
    })
}

func TestExtractDomain(t *testing.T) {
    Convey("Single domains can be extracted", t, func() {
        s, _ := extractDomain("pd.damouse/alpha")
        So(s, ShouldEqual, "pd.damouse")
    })
}

func TestExtractAction(t *testing.T) {
    Convey("Single actions can be extracted", t, func() {
        s, _ := extractActions("pd/alpha")
        So(s, ShouldEqual, "alpha")
    })
}