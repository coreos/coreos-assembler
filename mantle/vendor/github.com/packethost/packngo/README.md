# packngo
Packet Go Api Client

![](https://www.packet.net/media/labs/images/1679091c5a880faf6fb5e6087eb1b2dc/ULY7-hero.png)

Committing
----------

Before committing, it's a good idea to run `gofmt -w *.go`. ([gofmt](https://golang.org/cmd/gofmt/))


Usage
-----

This lib is used by the official [terraform-provider-packet](https://github.com/terraform-providers/terraform-provider-packet).

If you want to use it in your Go code, you can learn a lot from the `*_test.go` sources. Almost all out tests touch the Packet API, so you can see how auth, querying and POSTing works. For example [devices_test.go](devices_test.go).


Acceptance Tests
----------------

If you want to run tests against the actual Packet API, you must set envvar `PACKET_TEST_ACTUAL_API` to non-empty string for the `go test`. The device tests wait for the device creation, so it's best to run a few in parallel.

To run all the tests, you can do

```
$ PACKNGO_TEST_ACTUAL_API=1 go test -v -parallel 8
```

It's also useful to run only single acceptance test at a time:

```
$ PACKNGO_TEST_ACTUAL_API=1 go test -v -run=TestAccDeviceBasic
```
