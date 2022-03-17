#! /usr/bin/env node

const argv = require('minimist')(process.argv.slice(2))
const Fs = require('fs').promises
const Path = require('path')
const Chalk = require('chalk')
const Runner = require('./')

const DEFAULT_API_ADDR = '/dns4/node0.preload.ipfs.io/https'
const DEFAULT_BOOTSTRAP_ADDR = '/dns4/node0.preload.ipfs.io/tcp/443/wss/p2p/QmZMxNdpMkewiVZLMRxaNxUeZpDUb34pWjZ1kZvsd16Zic'

async function main () {
  if (argv.help || argv.h || argv.usage || argv.u) {
    return console.log(await Fs.readFile(Path.join(__dirname, 'usage.txt'), 'utf8'))
  }

  if (argv.version || argv.v) {
    return console.log(require('./package.json').version)
  }

  const data = argv.data || argv.d || `Test content created on ${new Date()}`
  const apiAddr = argv['api-addr'] || argv.a || DEFAULT_API_ADDR
  const bootstrapAddr = argv['bootstrap-addr'] || argv.b || DEFAULT_BOOTSTRAP_ADDR

  console.log(`🌎 Using preloader API Address: ${apiAddr}`)
  if (apiAddr === DEFAULT_API_ADDR) {
    console.log(Chalk.grey('(Use --api-addr arg to change)'))
  }

  console.log(`🥾 Using preloader Bootstrap Address: ${bootstrapAddr}`)
  if (bootstrapAddr === DEFAULT_BOOTSTRAP_ADDR) {
    console.log(Chalk.grey('(Use --bootstrap-addr arg to change)'))
  }

  console.log(`💾 Data that will be used in the test: "${data}"`)

  console.log('🏃‍♀️ Running the test...')

  console.time('Test execution time')
  let result

  try {
    result = await Runner.run({ data, apiAddr, bootstrapAddr })
  } finally {
    console.timeEnd('Test execution time')
  }

  if (result) {
    console.log(Chalk.green('🥳 This preloader is working as expected'))
  } else {
    console.log(Chalk.red('😭 This preloader is NOT working as expected'))
  }
}

main()
