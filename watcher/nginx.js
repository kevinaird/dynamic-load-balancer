const [ filename="./nginx.conf" ] = process.argv.slice(2);

const fs = require("fs");
const { generateNGINXConf } = require("./lib/staticNGINXConfig");

generateNGINXConf()
    .then(output => {
        fs.writeFileSync(filename,output,"utf-8");
        console.log(output);
    });