const Docker = require('dockerode');
const docker = new Docker();

async function findContainer(name) {
    const containers = await docker.listContainers({ all: true });
    const container = containers.find(
      a =>
        a.Names.find(b => {
          const n = b.replace(/^\//, "");
          return n == name;
        })
    );
    return await docker.getContainer(container.Id);
}

async function checkContainer(container) {
    let details = await container.inspect();
    const containerName = details.Name.slice(1);

    if (!details?.State?.Running) 
        throw new Error(`Container (${containerName}) is not running`);

    // Check for healthy state 5x
    let attempts = 0;
    while (details?.State?.Health?.Status != "healthy") {
        if (attempts > 5) throw new Error(`Container (${containerName}) is not healthy`);

        console.log(`Container (${containerName}) health status is ${details?.State?.Health?.Status}`);
        await new Promise(cb => setTimeout(cb,3000));
        details = await container.inspect();
    }

    return { containerName, details };
}

module.exports = {
    findContainer,
    checkContainer,
}