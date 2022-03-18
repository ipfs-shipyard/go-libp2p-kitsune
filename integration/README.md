# Integration tests

The integration test is inspired on [js-ipfs-preload-tester](https://github.com/mcamou/js-ipfs-preload-tester).
There are some differences:

- `js-ipfs-preload-tester` uses one IPFS node running in-process in Node.js and a
   second one running in a browser controlled via Puppeteer. This test runs both
   nodes in-process in Node.js. This is because the browser does not support
   WebSockets without a valid SSL certificate, which entails adding Nginx or a
   similar revers proxy into the mix, and I have been unable to make it play nice
   with a self-signed certificate.
- `js-ipfs-preload-tester` uses a short, timestamped string as the data. This test
   uses a 512K string. The string is created by generating a buffer of 256K random
   bytes and hex-encoding it. This is to test that IPLD navigation works correctly.
- `js-ipfs-preload-tester` does not disable MDNS in the js-ipfs nodes. This test
   does disable it to ensure that the js-ipfs nodes don't talk directly to each
   other and that all communication goes through the proxy even if everything is
   in the same LAN.

What the test does is:

- Start two js-ipfs nodes connected to a preload proxy.
- Add some content on one of the nodes.
- Fetch the CID from the other node and verify that the content matches.

## Running the test

1. You will need NodeJS
1. Start a go-ipfs node
1. Compile Kitsune with `go build`
1. Start Kitsune in preload mode: (This assumes that go-ipfs is listening on localhost:5001)

    `go-libp2p-kitsune -d /ip4/127.0.0.1/tcp/5001 -l /ip4/0.0.0.0/tcp/24001 -w /ip4/0.0.0.0/tcp/28080 -p 25001`

1. `cd integration/tester; npm install`
1. Start the integration test:

   `npm start -- --api-addr=/ip4/127.0.0.1/tcp/25001 --bootstrap-addr=/ip4/127.0.0.1/tcp/28080/ws/p2p/<Kistsune_peer_ID>`

If everything goes well, you should see something similar to the following:

``` text
🌎 Using preloader API Address: /ip4/127.0.0.1/tcp/25001
🥾 Using preloader Bootstrap Address: /ip4/127.0.0.1/tcp/28080/ws/p2p/Qma2SN9rV1JawUig6ydWYrM49xLXVVTqcKdMwsETqPCXME
🏃‍♀️ Running the test...
Node 1 ID: 12D3KooWGgXUbNVhRCxRyJGkpc4hricEQeK8Y5fwcbfc1RztdA8Y
Node 2 ID: 12D3KooWMkEdFjH3HdiwUdqVYfQyUMGti4FZReKwxdjEyUnTJKa2
CID:  CID(QmVDgAY8o9c7SoUoZxRb4ar1KRrmpqQoaogeKqg7bSZEYx)
Test execution time: 2.018s
🥳 This preloader is working as expected
```
