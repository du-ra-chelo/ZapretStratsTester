package main

/*
table inet ZST {

	    # Цепочка output: ДОБАВЛЯЕМ метку cgroup
	    chain output {
	        type filter hook output priority 0; policy accept;

	        # Добавляем метку к существующей (OR)
	        oifname IfaceWAN socket cgroupv2 level 0 "/test.slice/initSLEEP-1.scope" meta mark set mark | 0x01000000
	        oifname IfaceWAN socket cgroupv2 level 0 "/test.slice/initSLEEP-2.scope" meta mark set mark | 0x02000000
					#...
	        oifname IfaceWAN socket cgroupv2 level 0 "/test.slice/initSLEEP-15.scope" meta mark set mark | 0x0F000000
	    }

	    # Цепочка postnat: перехватываем только если НЕ обработано nfqws
	    chain postnat {
	        type filter hook postrouting priority srcnat+1; policy accept;

	        # Перехватываем только если НЕТ метки 0x40000000
	        # и ЕСТЬ метка от cgroup 0x04000000
	        oifname IfaceWAN meta mark & 0x40000000 == 0x00000000 meta mark & 0x0F000000 == 0x01000000 tcp dport {80,443} ct original packets 1-6 queue num 201 bypass
	        oifname IfaceWAN meta mark & 0x40000000 == 0x00000000 meta mark & 0x0F000000 == 0x01000000 udp dport 443 ct original packets 1-6 queue num 201 bypass

	        oifname IfaceWAN meta mark & 0x40000000 == 0x00000000 meta mark & 0x0F000000 == 0x02000000 tcp dport {80,443} ct original packets 1-6 queue num 202 bypass
	        oifname IfaceWAN meta mark & 0x40000000 == 0x00000000 meta mark & 0x0F000000 == 0x02000000 udp dport 443 ct original packets 1-6 queue num 202 bypass
					#...
	        oifname IfaceWAN meta mark & 0x40000000 == 0x00000000 meta mark & 0x0F000000 == 0x0F000000 tcp dport {80,443} ct original packets 1-6 queue num 215 bypass
	        oifname IfaceWAN meta mark & 0x40000000 == 0x00000000 meta mark & 0x0F000000 == 0x0F000000 udp dport 443 ct original packets 1-6 queue num 215 bypass
	    }

	    # Цепочка predefrag: notrack для ОБРАБОТАННЫХ пакетов
	    chain predefrag {
	        type filter hook output priority -401; policy accept;

	        # Пакеты с меткой nfqws не отслеживаем
	        mark & 0x40000000 != 0x00000000 notrack
	    }
	}
*/
const (
	tableName      = "ZST"
	tableTyp       = "inet"
	markNFQWS      = "0x40000000"
	markCGroup     = "0x0F000000"
	nftablePattern = `table ` + tableTyp + " " + tableName + ` {
	chain output {
	        type filter hook output priority 0; policy accept;
	}

	chain postnat {
		type filter hook postrouting priority srcnat + 1; policy accept;
	}

	chain predefrag {
		type filter hook output priority -401; policy accept;
		meta mark & ` + markNFQWS + ` != 0x00000000 notrack
	}

}`

	rulePostnatTemplate = `add rule ` + tableTyp + ` ` + tableName +
		` postnat oifname %s meta mark & ` + markNFQWS + ` == 0x00000000 meta mark & ` +
		markCGroup + ` == %s %s ct original packets 1-6 queue num %s bypass`
	ruleOutputTemplate = `add rule ` + tableTyp + ` ` + tableName +
		` output oifname %s socket cgroupv2 level 0 "%s" meta marak set mark | %s`

	tcp           = "tcp dport {80,443}"
	udp           = "udp dport 443"
	startQueueNum = 201
)
