#include <ngx_config.h>
#include <ngx_core.h>
#include <ngx_http.h>
#include <hiredis/hiredis.h>
#include <stdlib.h>
#include <time.h>

// Define the cache structure
typedef struct cache_entry_s {
    ngx_str_t key;
    time_t timestamp;
    ngx_int_t active;
    ngx_int_t in_use;
    redisReply *reply;
    struct cache_entry_s *next;
    struct cache_entry_s *prev;
} cache_entry_t;

const ngx_int_t CACHE_STATE_DEAD = 0;
const ngx_int_t CACHE_STATE_ACTIVE = 1;
const ngx_int_t CACHE_STATE_READYTOCLEAN = 2;
const ngx_int_t CACHE_STATE_CLEANINPROG = 3;

// Define the configuration structure
typedef struct {
    ngx_int_t debug_mode;
    ngx_str_t redis_host;
    ngx_int_t redis_port;
    ngx_int_t cache_ttl;
    cache_entry_t *cache;
} ngx_http_custom_loc_conf_t;

// Function declarations
static char* ngx_http_custom(ngx_conf_t *cf, ngx_command_t *cmd, void *conf);
static void* ngx_http_custom_create_loc_conf(ngx_conf_t *cf);
static ngx_int_t ngx_http_custom_handler(ngx_http_request_t *r);
static cache_entry_t* find_cache_entry(cache_entry_t *cache, ngx_str_t *key);
static cache_entry_t* add_cache_entry(ngx_pool_t *pool, cache_entry_t **cache, ngx_str_t *key, redisReply *reply);
static void ngx_http_custom_cleanup_cache(ngx_pool_t *pool, cache_entry_t *entry, cache_entry_t **cache);

// Configuration directives
static ngx_command_t ngx_http_custom_commands[] = {
    {
        ngx_string("custom"),
        NGX_HTTP_LOC_CONF|NGX_CONF_NOARGS,
        ngx_http_custom,
        NGX_HTTP_LOC_CONF_OFFSET,
        0,
        NULL
    },
    ngx_null_command
};

// Module context
static ngx_http_module_t ngx_http_custom_module_ctx = {
    NULL,                              /* preconfiguration */
    NULL,                              /* postconfiguration */

    NULL,                              /* create main configuration */
    NULL,                              /* init main configuration */

    NULL,                              /* create server configuration */
    NULL,                              /* merge server configuration */

    ngx_http_custom_create_loc_conf,   /* create location configuration */
    NULL                               /* merge location configuration */
};

// Module definition
ngx_module_t ngx_http_custom_module = {
    NGX_MODULE_V1,
    &ngx_http_custom_module_ctx,       /* module context */
    ngx_http_custom_commands,          /* module directives */
    NGX_HTTP_MODULE,                   /* module type */
    NULL,                              /* init master */
    NULL,                              /* init module */
    NULL,                              /* init process */
    NULL,                              /* init thread */
    NULL,                              /* exit thread */
    NULL,                              /* exit process */
    NULL,                              /* exit master */
    NGX_MODULE_V1_PADDING
};

// Create location configuration
static void* ngx_http_custom_create_loc_conf(ngx_conf_t *cf) {
    ngx_http_custom_loc_conf_t *conf;

    conf = ngx_pcalloc(cf->pool, sizeof(ngx_http_custom_loc_conf_t));
    if (conf == NULL) {
        return NGX_CONF_ERROR;
    }

    const char* REDIS_HOST = getenv("REDIS_HOST");
    const char* REDIS_PORT = getenv("REDIS_PORT");
    const char* DEBUG = getenv("DEBUG");
    const char* CACHE_TTL = getenv("CACHE_TTL");

    if (DEBUG == NULL) conf->debug_mode = 0;
    else conf->debug_mode = 1;

    if (REDIS_HOST == NULL) {
        conf->redis_host.len = 9;
        conf->redis_host.data = ngx_palloc(cf->pool, conf->redis_host.len);
        ngx_snprintf(conf->redis_host.data, conf->redis_host.len, "127.0.0.1");
    } else {
        conf->redis_host.len = strlen(REDIS_HOST);
        conf->redis_host.data = ngx_palloc(cf->pool, conf->redis_host.len);
        ngx_snprintf(conf->redis_host.data, conf->redis_host.len, "%s", REDIS_HOST);
    }

    if (REDIS_PORT == NULL) {
        conf->redis_port = 6379;
    } else {
        conf->redis_port = atoi(REDIS_PORT);
    }

    if (CACHE_TTL == NULL) {
        conf->cache_ttl = 10;
    } else {
        conf->cache_ttl = atoi(CACHE_TTL);
    }

    conf->cache = NULL;

    return conf;
}


// Find a cache entry
static cache_entry_t* find_cache_entry(cache_entry_t *cache, ngx_str_t *key) {
    if (cache == NULL || key == NULL) {
        return NULL;
    }

    cache_entry_t *entry = cache;
    while (entry) {
        if ( entry &&
            entry->active == CACHE_STATE_ACTIVE &&
            entry->key.len == key->len && 
            entry->key.data != NULL &&
            ngx_strncmp(entry->key.data, key->data, key->len) == 0
        ) {
            return entry;
        }
        entry = entry->next;
    }
    return NULL;
}

// Add a cache entry
static cache_entry_t* add_cache_entry(ngx_pool_t *pool, cache_entry_t **cache, ngx_str_t *key, redisReply *reply) {
    cache_entry_t *new_entry = ngx_palloc(pool, sizeof(cache_entry_t));
    if (new_entry == NULL) {
        return NULL;
    }

    new_entry->key.len = key->len;
    new_entry->key.data = ngx_palloc(pool, key->len);
    if (new_entry->key.data == NULL) {
        return NULL;
    }
    ngx_memcpy(new_entry->key.data, key->data, key->len);

    new_entry->timestamp = time(NULL);
    new_entry->reply = reply;
    new_entry->next = *cache;
    new_entry->prev = NULL;
    new_entry->active = CACHE_STATE_ACTIVE;
    new_entry->in_use = 0;

    if(new_entry->next != NULL) 
        new_entry->next->prev = new_entry;

    *cache = new_entry;

    return new_entry;
}

static void ngx_http_custom_free_cache_entry(void *data) {
    cache_entry_t *entry = data;
    if (entry->key.data) ngx_free(entry->key.data);
    if (entry->reply) freeReplyObject(entry->reply);
}

// Cleanup cache entry
static void ngx_http_custom_cleanup_cache(ngx_pool_t *pool, cache_entry_t *entry, cache_entry_t **cache) {
    if (entry->in_use > 0) return;
    
    if (entry->active != CACHE_STATE_READYTOCLEAN) return;
    entry->active = CACHE_STATE_CLEANINPROG;

    if (entry->next) {
        if (entry->prev) entry->next->prev = entry->prev;
        else entry->next->prev = NULL;
    }
    if (entry->prev) {
        if (entry->next) entry->prev->next = entry->next;
        else entry->prev->next = NULL;
    }
    if (*cache == entry) {
        if (entry->next) *cache = entry->next;
        else *cache = NULL;
    }
    
    entry->active = CACHE_STATE_DEAD;

    ngx_pool_cleanup_t *cln = ngx_pool_cleanup_add(pool, 0);
    if (cln != NULL) {
        cln->handler = ngx_http_custom_free_cache_entry;
        cln->data = entry;
    }
}

// Configuration function
static char* ngx_http_custom(ngx_conf_t *cf, ngx_command_t *cmd, void *conf) {
    ngx_http_core_loc_conf_t *clcf;

    // Get the location configuration
    clcf = ngx_http_conf_get_module_loc_conf(cf, ngx_http_core_module);

    // Set the handler
    clcf->handler = ngx_http_custom_handler;

    return NGX_CONF_OK;
}

// Request handler
static ngx_int_t ngx_http_custom_handler(ngx_http_request_t *r) {
    ngx_http_custom_loc_conf_t *conf;
    conf = ngx_http_get_module_loc_conf(r, ngx_http_custom_module);

    // Parse hostname
    if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "Server Port =%V", &r->headers_in.host->value);
    char hostname[r->headers_in.host->value.len+1];
    sprintf(hostname,"%s",r->headers_in.host->value.data);
    if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "hostname=%s", hostname);

    const char s[2] = ":";
    char* host = strtok(hostname,s);
    char* port = strtok(NULL,s);
    if (host == NULL || port == NULL) {
            ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "Failed to parse hostname: %s=%s:%s", hostname,host,port);
            return NGX_HTTP_INTERNAL_SERVER_ERROR;
    }
    if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "host=%s", host);
    if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "port=%s", port);    

    // Build the Cache key
    ngx_str_t key;
    key.len = strlen(port);
    key.data = ngx_palloc(r->pool, key.len);
    ngx_snprintf(key.data, key.len, "%s", port);
    if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "key=%V", &key);

    // Check the cache
    cache_entry_t *cache_entry = find_cache_entry(conf->cache, &key);
    time_t now = time(NULL);

    redisReply *reply;
    redisContext *c = NULL;

    if (cache_entry != NULL && (now - cache_entry->timestamp) < conf->cache_ttl) {
        if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "CACHE HIT");
        cache_entry->in_use++;
        reply = cache_entry->reply;
    }
    else {
        if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "CACHE MISS");

        if (cache_entry != NULL) {
            if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "cache entry lifespan=%d",now - cache_entry->timestamp);
            if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "Clean expired cache entry. in use=%d", cache_entry->in_use);
            cache_entry->active = CACHE_STATE_READYTOCLEAN;

            ngx_http_custom_cleanup_cache(r->pool, cache_entry,  &conf->cache);    // Register the cleanup function
        }

        if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "Connect to Redis");
        // Connect to Redis
        // redisContext *c = redisConnect("127.0.0.1", 6379);
        char redis_host[conf->redis_host.len+1];
        // sprintf(redis_host,"%s", conf->redis_host.data);
        memcpy(redis_host,conf->redis_host.data,conf->redis_host.len);
        redis_host[conf->redis_host.len] = '\0';
        if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "redis_host=%s",redis_host);
        if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "redis_port=%d",conf->redis_port);
        
        c = redisConnect(redis_host, conf->redis_port);
        if (c == NULL || c->err) {
            if (c) {
                ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "Redis connection error: %s", c->errstr);
                redisFree(c);
            } else {
                ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "Redis connection error: can't allocate redis context");
            }
            return NGX_HTTP_INTERNAL_SERVER_ERROR;
        }

        // Query Redis
        if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "Query Redis - frontend:%s", port);
        reply = redisCommand(c, "LRANGE frontend:%s 0 -1", port);
        if (reply == NULL) {
            ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "Redis command error");
            redisFree(c);
            return NGX_HTTP_INTERNAL_SERVER_ERROR;
        }
        // ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "redis reply = %sn", reply->str);

        // Check the reply type
        if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "Check the reply type");
        if (reply->type != REDIS_REPLY_ARRAY || reply->elements == 0) {
            freeReplyObject(reply);
            redisFree(c);
            ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "Redis reply error or empty list");
            return NGX_HTTP_INTERNAL_SERVER_ERROR;
        }

        // Save the response in the cache
        if (conf->cache_ttl != -1) {
            if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "Save the response in the cache");
            cache_entry = add_cache_entry(r->pool, &conf->cache, &key, reply);
            if (cache_entry != NULL) cache_entry->in_use++;
        }

        if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "Freeing redis connection...");
    }

    // Select a random backend
    if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "Select a random backend");
    srand(time(NULL));
    int index = rand() % reply->elements;
    char *backend = reply->element[index]->str;
    if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "backend: %s", backend);

    // Set the backend URL
    if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "Set the backend URL");

    ngx_str_t backend_url;
    backend_url.len = 1 + strlen(backend) + r->uri.len;
    if (conf->debug_mode==1) ngx_log_error(NGX_LOG_ERR, r->connection->log, 0, "backend_url.len=%d",backend_url.len);
    backend_url.data = ngx_palloc(r->pool, backend_url.len);

    ngx_snprintf(backend_url.data, backend_url.len, "/%s%V", backend, &r->uri);

    if (cache_entry != NULL) cache_entry->in_use--;
    if (c != NULL) redisFree(c);

    return ngx_http_internal_redirect(r, &backend_url, &r->args);

}