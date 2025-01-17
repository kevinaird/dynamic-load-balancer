const watcher = require("./lib/watchDockerEvents");
const { addMountebankInstance, dropMountebankInstance } = require("./lib/mountebankPool");

const mountebankImageName = process.env.MOUNTEBANK_IMAGE_NAME ?? "bbyars/mountebank";

watcher.events.on("health_status", event => {
    const containerName = event?.Actor?.Attributes?.name;
    const imageName = event?.Actor?.Attributes?.image;
    if (imageName != mountebankImageName) return;
    
    console.log("Got health_status event!", event?.Action, containerName, imageName);

    const status = event?.Action.split(": ")[1];

    if (status=="healthy") {
        addMountebankInstance(containerName);
    } else if (!!status) {
        dropMountebankInstance(containerName);
    }
});

watcher.events.on("die", event => {
    const containerName = event?.Actor?.Attributes?.name;
    const imageName = event?.Actor?.Attributes?.image;
    if (imageName != mountebankImageName) return;

    console.log("Got die event!", event?.Action, containerName);

    dropMountebankInstance(containerName);
});

watcher.events.on("kill", event => {
    const containerName = event?.Actor?.Attributes?.name;
    const imageName = event?.Actor?.Attributes?.image;
    if (imageName != mountebankImageName) return;

    console.log("Got kill event!", event?.Action, containerName);

    dropMountebankInstance(containerName);
});

watcher.events.on("oom", event => {
    const containerName = event?.Actor?.Attributes?.name;
    const imageName = event?.Actor?.Attributes?.image;
    if (imageName != mountebankImageName) return;

    console.log("Got oom event!", event?.Action, containerName);

    dropMountebankInstance(containerName);
});

watcher.events.on("pause", event => {
    const containerName = event?.Actor?.Attributes?.name;
    const imageName = event?.Actor?.Attributes?.image;
    if (imageName != mountebankImageName) return;

    console.log("Got pause event!", event?.Action, containerName);

    dropMountebankInstance(containerName);
});

watcher.events.on("unpause", event => {
    const containerName = event?.Actor?.Attributes?.name;
    const imageName = event?.Actor?.Attributes?.image;
    if (imageName != mountebankImageName) return;

    console.log("Got pause event!", event?.Action, containerName);

    addMountebankInstance(containerName);
});

watcher.start({
    filters: JSON.stringify({ 
        images: { [mountebankImageName]: true },
    })
});

console.log("Mountebank Instance Watcher started.");