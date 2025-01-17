const Redis = require("ioredis");
const axios = require('axios');

const { findContainer, checkContainer } = require("./containers");

const redis = new Redis({
    host: process.env.REDIS_HOST,
    port: process.env.REDIS_PORT,
});
const mountebankPort = process.env.MOUNTEBANK_PORT ?? 2525;

async function healthCheck(containerName) {
    const container = await findContainer(containerName);
    await checkContainer(container);

    const res = await axios.get(`http://${containerName}:${mountebankPort}/imposters`,{
        timeout: 1000,
    });
    console.log("health check response:",containerName,res.data);
    if (!res.data?.imposters)
        throw new Error(`Container (${containerName}) was unreachable on port ${mountebankPort}`);
}

async function addMountebankInstance(containerName) {
    try {
        console.log("addMountebankInstance",containerName);
        await healthCheck(containerName);

        const endpoint = `http://${containerName}:${mountebankPort}`;

        // Remove incase its already in redis
        await redis.lrem("mountebank_pool",0,endpoint);
        await redis.lrem("frontend:2525",0,endpoint);
        
        // if not - add it to redis
        await redis.rpush("mountebank_pool",endpoint);
        await redis.rpush("frontend:2525",endpoint);

    } catch(err) {
        console.error("Error adding mountebank instance", containerName, err);
    }
}

async function dropMountebankInstance(containerName) {
    try {
        console.log("dropMountebankInstance",containerName);

        const endpoint = `http://${containerName}:${mountebankPort}`;

        await redis.lrem("mountebank_pool",0,endpoint);
        await redis.lrem("frontend:2525",0,endpoint);
    } catch(err) {
        console.error("Error dropping mountebank instance", containerName, err);
    }
}

module.exports = {
    healthCheck,
    addMountebankInstance,
    dropMountebankInstance,
};