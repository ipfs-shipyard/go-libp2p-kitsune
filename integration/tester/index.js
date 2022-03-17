const log = require('debug')('ipfs-preload-tester:runner')
const IPFS = require('ipfs-core')
const fs = require('fs')
const os = require('os')
const path = require('path')

process.env["NODE_TLS_REJECT_UNAUTHORIZED"] = 0;

exports.run = async ({ data, apiAddr, bootstrapAddr }) => {
  tmpdir = fs.mkdtempSync(path.join(os.tmpdir(), `preload-tester-`))

  const ipfsConfig = (index) => {
    offset = Math.trunc( Math.random() * 1000 )
    repo = path.join(tmpdir, `repo-${index}`)
    return {
      preload: {
        enabled: true,
        addresses: [apiAddr]
      },
      silent: true,
      repo,
      config: {
        Addresses: {
          Swarm: [
            `/ip4/127.0.0.1/tcp/${4002+offset}`,
            `/ip4/127.0.0.1/tcp/${4002+offset+1}/ws`,
          ],
        },
        Bootstrap: [bootstrapAddr],
        // Turn MDNS off so the nodes don't discover each other nor any other nodes in the LAN
        Discovery: {
          MDNS: {
            Enabled: false
          }
        }
      }
    }
  }

  log('Creating IPFS nodes...')

  // While it would be better to create a browser and a Node node (as in js-ipfs-tester), for this
  // we use 2 Node nodes to get around needing an SSL certificate for the browser node
  const ipfs1 = await IPFS.create(ipfsConfig(1))
  const ipfs2= await IPFS.create(ipfsConfig(2))

  console.log(`Node 1 ID: ${(await ipfs1.id()).id}`)
  console.log(`Node 2 ID: ${(await ipfs2.id()).id}`)

  console.log(`Adding data: "${data}"`)
  const { cid } = await ipfs1.add(data)

  console.log(`CID: `, cid)

  const chunks = []

  for await (const chunk of ipfs2.cat(cid)) {
    log('Got a chunk "%s"', chunk)
    chunks.push(chunk)
  }

  await Promise.all([
    ipfs1.stop(),
    ipfs2.stop(),
  ])

  return Buffer.concat(chunks).toString() === data
}
