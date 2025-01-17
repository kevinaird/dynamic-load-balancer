module("hipache", package.seeall)

local _M = {}

-- Setup Redis Client
local redis = require "resty.redis"
local REDIS_HOST = os.getenv("REDIS_HOST") and os.getenv("REDIS_HOST") or "redis"
local REDIS_PORT = os.getenv("REDIS_PORT") and tonumber(os.getenv("REDIS_PORT")) or 6379
local red = redis:new()
red:set_timeout(1000)
-- local redis_connected = false

-- Setup LRU Cache
local lrucache = require "resty.lrucache"
local c, err = lrucache.new(200)  -- allow up to 200 items in the cache
if not c then
    error("failed to create the cache: " .. (err or "unknown"))
end


function _M.go()
    local backends = {}
    -- local deads = {}

    -- Check LRU Cache
    local cached_backends = c:get(ngx.var.server_port)
    if cached_backends then 
        backends = cached_backends
    else
        -- Cache Miss
        -- Connect to Redis
        local ok, err = red:connect(REDIS_HOST, REDIS_PORT)
        if not ok then
            ngx.say("Failed to connect to Redis: ", err)
            return
        end

        -- Redis lookup
        red:multi()
        red:lrange("frontend:" .. ngx.var.server_port, 0, -1)
        red:smembers("dead:" .. ngx.var.server_port)
        local ans, err = red:exec()
        if not ans then
            ngx.say("Lookup failed: ", err)
            return
        end

        -- Parse the result of the Redis lookup
        backends = ans[1]
        if #backends == 0 then
            backends = ans[2]
        end
        if #backends == 0 then
            ngx.say("Backend not found")
            return
        end

        -- Save backends in LRU Cache
        c:set(ngx.var.server_port, backends)

        -- deads = ans[3]
        -- if not deads then
        --     deads = {}
        -- end
    
    end

    -- Pickup a random backend (after removing the dead ones)
    local indexes = {}
    -- for i, v in ipairs(deads) do
    --     deads[v] = true
    -- end

    for i, v in ipairs(backends) do
        -- if deads[tostring(i)] == nil then
            table.insert(indexes, i)
        -- end
    end

    local index = indexes[math.random(1, #indexes)]
    local backend = backends[index]

    -- handle direct calls to a specific instance i.e. /!backendname/uri
    ngx.var.backend_uri = ngx.var.request_uri
    if string.sub(ngx.var.request_uri,1,2) == "/!" then
        for instanceName in string.gmatch(ngx.var.request_uri,"([^/]+)") do
            local instanceName2 = "http://" .. string.sub(instanceName,2,string.len(instanceName))
            for k, v in pairs(backends) do
                if string.sub(v,1,string.len(instanceName2)) == instanceName2 then
                    backend = v
                    ngx.var.backend_uri = string.sub(ngx.var.request_uri,string.len(instanceName)+2, string.len(ngx.var.request_uri))
                    break
                end
            end
        break
        end
    end

    -- Announce dead backends if there is any
    -- local deads = ngx.shared.deads
    -- for i, v in ipairs(deads:get_keys()) do
    --     red:publish("dead", deads:get(v))
    --     deads:delete(v)
    -- end

    -- Set the connection pool (to avoid connect/close everytime)
    red:set_keepalive(0, 100)

    -- Export variables
    ngx.var.backend = backend
    ngx.var.backends_len = #backends
    -- ngx.var.backend_id = index - 1
    -- ngx.var.frontend = frontend
    -- ngx.var.vhost = vhost_name
    if ngx.var.backend_uri == "" then
        ngx.var.backend_uri = "/"
    end
end

return _M