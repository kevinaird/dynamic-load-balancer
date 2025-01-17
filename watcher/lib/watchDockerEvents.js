const Docker = require('dockerode');
const EventEmitter = require('node:events');

const docker = new Docker();
const eventEmitter = new EventEmitter();

const watchDockerEvents = (opts = {}) => new Promise((resolve,reject) => {
    docker.getEvents(opts, (err, stream) => {
        if (err) {
            console.error("Error streaming events",err);
            return reject(err);
        }
        if (!stream) {
            console.error("Failed to create stream");
            return reject("Failed to create stream");
        }

        const finish = () => {
            console.log("getEvents finish called.");
            stream.removeListener("data", dataListener);
            stream.removeListener("end", endListener);
            return resolve();
        };

        const parseEvent = (rawEvent) => {
            try {
                const event = JSON.parse(rawEvent);
                return [event];
            } catch(ex) {
                try {
                    const events = rawEvent
                        .trim()
                        .split("\n")
                        .map(raw => JSON.parse(raw));
                    return events;
                } catch(ex) {
                    console.warn("Failed to parse host event");
                    console.log(rawEvent.toString());
                    return undefined;
                }
            }
        };

        const dataListener = (rawEvent) => {
            const events = parseEvent(rawEvent);
            if (!events) return;

            for (const event of events) {
                const action = `${event?.Action}`.split(":")[0]
                //console.log("docker event!", action, event?.Actor?.Attributes?.name);
                eventEmitter.emit(action, event);
            }
        };

        const endListener = () => {
            console.log("getEvents end called.");
            finish();
        };

        stream.on("data", dataListener);
        stream.on("end", endListener);
    })
});

const watchDockerEventsIndef = async (opts) => {
    while(true) {
        await watchDockerEvents(opts)
            .catch(err => console.error("Caught error",err));
    }
};

module.exports = {
    start: (opts) => watchDockerEventsIndef(opts),
    events: eventEmitter,
};