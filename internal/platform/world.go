package platform

import (
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/spark/internal/metric"
	"github.com/golem-mc/spark/internal/sparkproto"
	"github.com/magiconair/properties"
)

// worldName is how the viewer names a world: its dimension key.
func worldName(w plugin.World) string { return w.Key().String() }

// entityTypeName is an entity type's name as Bukkit's EntityType.getName
// gives it: the key's path for minecraft's, else the whole key.
func (p *Platform) entityTypeName(e plugin.Entity) string {
	key, ok := p.Server.Registries().KeyOf(e.Type())
	if !ok {
		return ""
	}
	if key.Namespace == "minecraft" {
		return key.Path
	}
	return key.String()
}

// counts are the players, entities, block entities and loaded chunks of
// every world; call it on the tick goroutine. spark: WorldInfoProvider.pollCounts
func (p *Platform) counts() metric.WorldCounts {
	c := metric.WorldCounts{Players: int32(len(p.Server.Players()))}
	for _, w := range p.Server.Worlds() {
		for _, chunk := range w.LoadedChunks() {
			c.Chunks++
			c.Entities += int32(len(chunk.Entities()))
			c.TileEntities += int32(len(chunk.BlockEntities()))
		}
	}
	return c
}

// chunkInfo is a loaded chunk's entity counts by type name.
type chunkInfo struct {
	x, z   int32
	counts map[string]int32
	total  int32
}

// worldStatistics are the entities of every loaded chunk, grouped into
// regions of touching chunks; call it on the tick goroutine.
// spark: WorldStatisticsProvider.getWorldStatistics
func (p *Platform) worldStatistics() *sparkproto.WorldStatistics {
	stats := &sparkproto.WorldStatistics{EntityCounts: map[string]int32{}}
	worlds := p.Server.Worlds()
	stats.GameRules = p.gameRules(worlds)
	stats.DataPacks = p.dataPacks()
	for _, w := range worlds {
		var chunks []chunkInfo
		for _, chunk := range w.LoadedChunks() {
			info := chunkInfo{x: int32(chunk.Pos().X), z: int32(chunk.Pos().Z), counts: map[string]int32{}}
			for _, e := range chunk.Entities() {
				name := p.entityTypeName(e)
				if name == "" {
					name = "unknown[" + e.Type().String() + "]"
				}
				info.counts[name]++
				info.total++
			}
			chunks = append(chunks, info)
		}
		world := &sparkproto.WorldStatistics_World{Name: worldName(w)}
		for _, region := range groupIntoRegions(chunks) {
			r := &sparkproto.WorldStatistics_Region{}
			for _, c := range region {
				r.TotalEntities += c.total
				r.Chunks = append(r.Chunks, &sparkproto.WorldStatistics_Chunk{X: c.x, Z: c.z, TotalEntities: c.total, EntityCounts: c.counts})
				for name, n := range c.counts {
					stats.EntityCounts[name] += n
				}
			}
			world.TotalEntities += r.TotalEntities
			world.Regions = append(world.Regions, r)
		}
		stats.TotalEntities += world.TotalEntities
		stats.Worlds = append(stats.Worlds, world)
	}
	return stats
}

// gameRules are the game rules, with their defaults and each world's value.
// spark: PaperWorldInfoProvider.pollGameRules
func (p *Platform) gameRules(worlds []plugin.World) []*sparkproto.WorldStatistics_GameRule {
	var out []*sparkproto.WorldStatistics_GameRule
	for _, info := range p.Server.Registries().GameRules() {
		rule := &sparkproto.WorldStatistics_GameRule{Name: gameRuleName(info.Key), DefaultValue: gameRuleValue(info, info.Default), WorldValues: map[string]string{}}
		for _, w := range worlds {
			rule.WorldValues[worldName(w)] = gameRuleValue(info, w.GameRule(info.ID))
		}
		out = append(out, rule)
	}
	return out
}

// gameRuleName is a rule's name as /gamerule takes it: the path of a
// vanilla rule's key.
func gameRuleName(k plugin.Key) string {
	if k.Namespace == "minecraft" {
		return k.Path
	}
	return k.String()
}

func gameRuleValue(info plugin.GameRuleInfo, v int) string {
	if info.Boolean {
		return strconv.FormatBool(v != 0)
	}
	return strconv.Itoa(v)
}

// dataPacks are the data packs the server knows, enabled or not.
// spark: PaperWorldInfoProvider.pollDataPacks
func (p *Platform) dataPacks() []*sparkproto.WorldStatistics_DataPack {
	var out []*sparkproto.WorldStatistics_DataPack
	for _, pack := range p.Server.DataPacks() {
		// Paper's Datapack.Source names, lower case without underscores.
		source := map[plugin.DataPackSource]string{plugin.DataPackBuiltIn: "builtin", plugin.DataPackFeature: "feature", plugin.DataPackWorld: "world"}[pack.Source]
		out = append(out, &sparkproto.WorldStatistics_DataPack{Name: pack.Title.PlainString(), Description: pack.Description.PlainString(), Source: source})
	}
	return out
}

// groupIntoRegions groups the chunks that have entities into regions of
// chunks touching each other, diagonals included, in the order the first
// chunk of each was found. spark: WorldStatisticsProvider.groupIntoRegions
func groupIntoRegions(chunks []chunkInfo) [][]chunkInfo {
	type coord struct{ x, z int32 }
	byPos := map[coord]chunkInfo{}
	var order []coord
	for _, c := range chunks {
		if c.total == 0 {
			continue
		}
		k := coord{c.x, c.z}
		if _, dup := byPos[k]; !dup {
			order = append(order, k)
		}
		byPos[k] = c
	}
	var regions [][]chunkInfo
	for _, start := range order {
		first, ok := byPos[start]
		if !ok {
			continue
		}
		delete(byPos, start)
		region := []chunkInfo{first}
		for queue := []chunkInfo{first}; len(queue) > 0; {
			c := queue[0]
			queue = queue[1:]
			for dz := int32(-1); dz <= 1; dz++ {
				for dx := int32(-1); dx <= 1; dx++ {
					k := coord{c.x + dx, c.z + dz}
					if adj, ok := byPos[k]; ok && (dx|dz) != 0 {
						delete(byPos, k)
						region = append(region, adj)
						queue = append(queue, adj)
					}
				}
			}
		}
		regions = append(regions, region)
	}
	return regions
}

// hiddenConfigPaths are the server.properties keys never uploaded; a "."
// in a key is written "<dot>". spark: ServerConfigProvider.BASE_HIDDEN_PATHS
var hiddenConfigPaths = []string{
	"server-ip", "motd", "resource-pack", "rcon<dot>password", "rcon<dot>ip", "level-seed",
	"management-server-secret", "management-server-tls-keystore-password",
}

// serverConfigurations are the server's configuration files as JSON, by
// file name, without the hidden keys: server.properties, the only file golem
// reads. spark: ServerConfigProvider.export
func serverConfigurations() map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile("server.properties")
	if err != nil {
		return out
	}
	props, err := properties.Load(data, properties.UTF8)
	if err != nil {
		return out
	}
	values := map[string]any{}
	for _, k := range props.Keys() {
		v, _ := props.Get(k)
		values[k] = typedProperty(v)
	}
	for _, hidden := range hiddenConfigPaths {
		delete(values, strings.ReplaceAll(hidden, "<dot>", "."))
	}
	if j, err := json.Marshal(values); err == nil {
		out["server.properties"] = string(j)
	}
	return out
}

// typedProperty is a property value as spark types it: a boolean, a whole
// number or a string. spark: PropertiesConfigParser
func typedProperty(v string) any {
	switch v {
	case "true":
		return true
	case "false":
		return false
	}
	if v != "" && !slices.ContainsFunc([]byte(v), func(b byte) bool { return b < '0' || b > '9' }) && len(v) < 19 {
		var n int64
		for _, b := range []byte(v) {
			n = n*10 + int64(b-'0')
		}
		return n
	}
	return v
}
