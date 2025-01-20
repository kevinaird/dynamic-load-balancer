const [ filename, capacity=1, mode="virtual" ] = process.argv.slice(2);

console.log("cwd=", process.cwd());
console.log("filename=",filename);
console.log("capacity=",capacity);
console.log("mode=",mode);

const path = require("path");
const { deployServiceFromFile } = require("./lib/deployer");

(async () => {
    await new Promise(cb => setTimeout(cb, 5000));
    await deployServiceFromFile(
        path.join(process.cwd(), filename),
        { capacity: parseInt(""+capacity), mode },
    );
})()
.then(() => console.log("Deployment complete!"))
.catch(err => console.error("Error occurred during deployment",err.message));