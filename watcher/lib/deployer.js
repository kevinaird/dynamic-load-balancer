const { URL } = require('node:url');
const axios = require('axios');
const fs = require("fs").promises;

async function deployServiceFromFile(filename, { capacity, mode }) {
    const imposterStr = await fs.readFile(filename,"utf-8");
    const imposter = JSON.parse(imposterStr);

    await deployService({ imposter, capacity, mode });
}

async function deployService(conf) {
    let instances = [];

    if (process.env.REDIS_HOST) {
        const { getMountebankInstances } = require("./mountebankPool");
        instances = await getMountebankInstances();
        instances = instances.map(mb => mb.match(/^http:\/\/([^\:]+):[0-9]+$/)[1]);
    } else {
        const { listContainerNames } = require("./containers");
        instances = await listContainerNames(/^dynamic-load-balancer-mb-[0-9]+$/);
    }

    const { capacity, imposter, mode } = conf;

    if (mode == "debug") 
        instances = instances.filter(mb => /mb-debug/.test(mb));
    else 
        instances = instances.filter(mb => !/mb-debug/.test(mb));

    instances = instances.slice(0,capacity);
    
    if (instances.length == 0)
        throw new Error("No instances to deploy to");

    if (process.env.REDIS_HOST) {
        const { redis } = require("./mountebankPool");
        await redis.del(`frontend:${imposter.port}`);
    }

    const output = await Promise.allSettled(
        instances.map(instance => deployImposter(imposter, instance))
    );

    console.log("output",output);
    const firstFail = output.find(o => o.status == "rejected");
    if (firstFail) {
        console.log(firstFail.reason.response.data);
        throw new Error(firstFail.reason);
    }
    
    await redis.publish("new_frontend",`${imposter.port}`);
}

async function deployImposter(imposter, instance) {
    console.log("deploying to", instance, "on port", imposter.port, "...")

    const mb_url = process.env.MASTER_NGINX ?
        `${process.env.MASTER_NGINX}:2525/${instance}`
        : `http://${instance}:2525`;
    console.log("mb_url=", mb_url);

    // 0. Make sure port is clear
    await axios.delete(`${mb_url}/imposters/${imposter.port}`)

    // 1. Deploy to Mountebanks
    await axios.post(`${mb_url}/imposters`,imposter);

    // 2. Configure Load Balancer
    const endpoint = `http://${instance}:${imposter.port}`;

    if (process.env.REDIS_HOST) {
        const { redis } = require("./mountebankPool");
        await redis.lrem(`frontend:${imposter.port}`,0,endpoint);
        await redis.rpush(`frontend:${imposter.port}`,endpoint);
    } else {
        console.log("Skipped redis config update since REDIS_HOST is not set");
    }

    console.log("deployed to", instance, "on port", imposter.port)
}

module.exports = {
    deployServiceFromFile,
    deployService,
}