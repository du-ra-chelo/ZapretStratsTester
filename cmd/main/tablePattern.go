package main

/*
IFACE_WAN="wan"

nft create table inet zst

nft add chain inet zst postnat "{type filter hook postrouting priority srcnat+1;}"
nft add rule inet zst postnat oifname $IFACE_WAN meta mark and 0x40000000 == 0x00000000 tcp dport "{80,443}" ct original packets 1-6 queue num 200 bypass
nft add rule inet zst postnat oifname $IFACE_WAN meta mark and 0x40000000 == 0x00000000 udp dport 443 ct original packets 1-6 queue num 200 bypass

nft add chain inet zst predefrag "{type filter hook output priority -401;}"
nft add rule inet zst predefrag mark \& 0x40000000 != 0x00000000 notrack
*/

const (
	tableName      = "ZST"
	tableTyp       = "inet"
	nftablePattern = `table ` + tableTyp + " " + tableName + ` {
	chain postnat {
		type filter hook postrouting priority srcnat + 1;
		}
	chain predefrag {
		type filter hook output priority -401; policy accept;
		meta mark & 0x40000000 != 0x00000000 notrack
	}
}`
)
