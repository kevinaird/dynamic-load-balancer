module("hipache", package.seeall)

local _M = {}

-- Setup Redis Client
local redis = require "resty.redis"
local REDIS_HOST = "redis" -- os.getenv("REDIS_HOST")
local REDIS_PORT = 6379 -- os.getenv("REDIS_PORT")
local red = redis:new()
red:set_timeout(1000)
local redis_connected = false

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
        if not redis_connected then
            local ok, err = red:connect(REDIS_HOST, REDIS_PORT)
            if not ok then
                ngx.say("Failed to connect to Redis: ", err)
                return
            end
            redis_connected = true
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
end

return _M