package logfmt

import (
	"bytes"
	"log"
	"reflect"
	"sort"
	"testing"
)

type kv struct{ key, val []byte }

func (k kv) String() string { return string(k.key) + "=" + string(k.val) }

type byKeyName []kv

func (b *byKeyName) visit(key, val []byte) bool {
	*b = append(*b, kv{key, val})
	log.Printf("visit(%q, %q) -> b=%v", string(key), string(val), b)
	return true
}
func (b byKeyName) Len() int           { return len(b) }
func (b byKeyName) Less(i, j int) bool { return bytes.Compare(b[i].key, b[j].key) == -1 }
func (b byKeyName) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }

func TestScanKeyValue(t *testing.T) {
	var tests = []struct {
		input         string
		allowEmptyKey bool
		keepGarbage   bool
		want          []kv
	}{
		{
			input: "hello=bye",
			want: []kv{
				{key: []byte("hello"), val: []byte("bye")},
			},
		},
		{
			input: "hello=bye crap crap",
			want: []kv{
				{key: []byte("hello"), val: []byte("bye crap crap")},
			},
		},
		{
			input: "hello=bye crap crap    ",
			want: []kv{
				{key: []byte("hello"), val: []byte("bye crap crap    ")},
			},
		},
		{
			input: "hello=bye crap crap allo=more crap",
			want: []kv{
				{key: []byte("hello"), val: []byte("bye crap crap")},
				{key: []byte("allo"), val: []byte("more crap")},
			},
		},
		{
			input: `hello="bye crap crap" allo=more crap`,
			want: []kv{
				{key: []byte("hello"), val: []byte("bye crap crap")},
				{key: []byte("allo"), val: []byte("more crap")},
			},
		},
		// {
		// 	input: `hello="bye crap\" crap" allo=more crap`,
		// 	want: []kv{
		// 		{key: []byte("hello"), val: []byte("bye crap\" crap")},
		// 		{key: []byte("allo"), val: []byte("more crap")},
		// 	},
		// },
		// {
		// 	input: `hello="bye crap\\" allo=more crap`,
		// 	want: []kv{
		// 		{key: []byte("hello"), val: []byte("bye crap\\")},
		// 		{key: []byte("allo"), val: []byte("more crap")},
		// 	},
		// },
		{
			input: " hello=bye",
			want: []kv{
				{key: []byte("hello"), val: []byte("bye")},
			},
		},
		{
			input: " hello=bye crap crap",
			want: []kv{
				{key: []byte("hello"), val: []byte("bye crap crap")},
			},
		},
		{
			input: " hello=bye crap crap    ",
			want: []kv{
				{key: []byte("hello"), val: []byte("bye crap crap    ")},
			},
		},
		{
			input: " hello=bye crap crap allo=more crap",
			want: []kv{
				{key: []byte("hello"), val: []byte("bye crap crap")},
				{key: []byte("allo"), val: []byte("more crap")},
			},
		},
		{
			input: ` hello="bye crap crap" allo=more crap`,
			want: []kv{
				{key: []byte("hello"), val: []byte("bye crap crap")},
				{key: []byte("allo"), val: []byte("more crap")},
			},
		},

		{
			input: ` hello="bye crap=crap" allo=more crap`,
			want: []kv{
				{key: []byte("hello"), val: []byte("bye crap=crap")},
				{key: []byte("allo"), val: []byte("more crap")},
			},
		},
		{
			input:       ` hello="bye crap=crap" allo=more crap`,
			keepGarbage: true,
			want: []kv{
				{key: []byte("garbage"), val: []byte(" ")},
				{key: []byte("hello"), val: []byte("bye crap=crap")},
				{key: []byte("allo"), val: []byte("more crap")},
			},
		},

		// sanitized real world input that breaks kr/logfmt
		// {
		// 	input: `time="Wed Nov  5 16:37:44 2014" pid="838383" level="1" version="ohwat" somekey="somevalue98754" msg="ohai_something --something_id='777262626' --else_id='67876789876' --something_name='derpderp' --else_flag='somevalue' --something='gyhjnhgvbhjnhuygvbhnjuygvbhnjygvbhnjkiuhygbhnjkmjhygtfrdedrtyuijkmnbvcfgyhujkmn bvftgyhujkm' --dritdirt='ghjkjhgcvghjkjhb' --hello_integer='1' --chienvache='7654' --drit_vache_spelling='romeo-julliet-111.1.2-190001010130.thing.things.thingz' --hellothing='watwat=herpherp,takcok=ff:ff:ff:ff:ff:ff,vif=animal67876789876,lolthing=127.0.0.1,halloweenmask=255.255.192.0,google_is_that_u=8.8.8.8,lolthingv6=,lolthingwatmask=,lolthingwatgw=,lolthingv4vlan=8888,lolthingwatland=' --hello_path_thingy_maybe='somevaluedirt1:/love_doge/dirt1\nsomevaluedirt2:/love_doge/dirt2\nsomevaluedirt3:/love_doge/dirt3\nsomevaluedirt4:/love_doge/dirt4\nsomevaluedirt5:/love_doge/dirt5\nsomevaluedirt6:/love_doge/dirt6\nsomevaluedirt7:/love_doge/dirt7\nsomevaluedirt8:/love_doge/dirt8\nsomevaluedirt9:/love_doge/dirt9\nsomevaluedirt10:/love_doge/dirt10\nsomevaluedirt11:/love_doge/dirt11\nsomevaluedirt12:/love_doge/dirt12\nsomevaluedirt13:/love_doge/dirt13\nsomevaluedirt14:/love_doge/dirt14\nsomevaluedirt15:/love_doge/dirt15' --hell_can_i_haz='9000' --cookies='42' --joy_disabled='0' --happyness_disabled='0' --dirtbagotry='canonical_canon=canoncanon@canon.canon\nfirst_landing_site=/\nlast_landing_site=/\nwatwat_something=true\njoy_score=0.448474747474747474\nmagic_thegathering=' --hello_thing_dirt='#itakcokomment\n\nlolwat: derpderp' --chien_vache_prerequis='dritthing=wat-thing,something=wat-thing,vachechien=wat-thing,oh_hai=wat-thing,happy_table=watdat'"`,
		// 	want: []kv{
		// 		{key: []byte("time"), val: []byte("Wed Nov  5 16:37:44 2014")},
		// 		{key: []byte("pid"), val: []byte("838383")},
		// 		{key: []byte("level"), val: []byte("1")},
		// 		{key: []byte("version"), val: []byte("ohwat")},
		// 		{key: []byte("somekey"), val: []byte("somevalue98754")},
		// 		{key: []byte("msg"), val: []byte("ohai_something --something_id='777262626' --else_id='67876789876' --something_name='derpderp' --else_flag='somevalue' --something='gyhjnhgvbhjnhuygvbhnjuygvbhnjygvbhnjkiuhygbhnjkmjhygtfrdedrtyuijkmnbvcfgyhujkmn bvftgyhujkm' --dritdirt='ghjkjhgcvghjkjhb' --hello_integer='1' --chienvache='7654' --drit_vache_spelling='romeo-julliet-111.1.2-190001010130.thing.things.thingz' --hellothing='watwat=herpherp,takcok=ff:ff:ff:ff:ff:ff,vif=animal67876789876,lolthing=127.0.0.1,halloweenmask=255.255.192.0,google_is_that_u=8.8.8.8,lolthingv6=,lolthingwatmask=,lolthingwatgw=,lolthingv4vlan=8888,lolthingwatland=' --hello_path_thingy_maybe='somevaluedirt1:/love_doge/dirt1\nsomevaluedirt2:/love_doge/dirt2\nsomevaluedirt3:/love_doge/dirt3\nsomevaluedirt4:/love_doge/dirt4\nsomevaluedirt5:/love_doge/dirt5\nsomevaluedirt6:/love_doge/dirt6\nsomevaluedirt7:/love_doge/dirt7\nsomevaluedirt8:/love_doge/dirt8\nsomevaluedirt9:/love_doge/dirt9\nsomevaluedirt10:/love_doge/dirt10\nsomevaluedirt11:/love_doge/dirt11\nsomevaluedirt12:/love_doge/dirt12\nsomevaluedirt13:/love_doge/dirt13\nsomevaluedirt14:/love_doge/dirt14\nsomevaluedirt15:/love_doge/dirt15' --hell_can_i_haz='9000' --cookies='42' --joy_disabled='0' --happyness_disabled='0' --dirtbagotry='canonical_canon=canoncanon@canon.canon\nfirst_landing_site=/\nlast_landing_site=/\nwatwat_something=true\njoy_score=0.448474747474747474\nmagic_thegathering=' --hello_thing_dirt='#itakcokomment\n\nlolwat: derpderp' --chien_vache_prerequis='dritthing=wat-thing,something=wat-thing,vachechien=wat-thing,oh_hai=wat-thing,happy_table=watdat'")},
		// 	},
		// },
	}

	for n, tt := range tests {
		t.Logf("test #%d", n)
		var got byKeyName
		if !Parse([]byte(tt.input), tt.allowEmptyKey, tt.keepGarbage, (&got).visit) {
			t.Fatalf("should have been able to parse: %q", tt.input)
		}
		sort.Sort(byKeyName(tt.want))
		sort.Sort(got)
		if !reflect.DeepEqual(tt.want, []kv(got)) {
			t.Logf("want=%v", tt.want)
			t.Logf(" got=%v", got)
			t.Fatalf("different KVs for %q", tt.input)
		}
	}
}

func TestFindWordFollowedBy(t *testing.T) {
	var tests = []struct {
		input         string
		from          int
		found         bool
		allowEmptyKey bool
		want          string
	}{
		{
			input: "hello=bye",
			from:  0,
			found: true,
			want:  "hello",
		},
		{
			input: "hello=bye aloa allo=bye",
			from:  6,
			found: true,
			want:  "allo",
		},
		{
			input: "hello=bye aloa allo.fr=bye",
			from:  6,
			found: true,
			want:  "allo.fr",
		},
		{
			input: " hello=bye",
			from:  0,
			found: true,
			want:  "hello",
		},
		{
			input: " hello=bye",
			from:  1,
			found: true,
			want:  "hello",
		},
		{
			input: "allo hello=bye",
			from:  0,
			found: true,
			want:  "hello",
		},
		{
			input: "allo hello=bye",
			from:  1,
			found: true,
			want:  "hello",
		},
		{
			input: " allo hello=bye",
			from:  0,
			found: true,
			want:  "hello",
		},
		{
			input: " allo hello=bye",
			from:  1,
			found: true,
			want:  "hello",
		},
		{
			input: " hello ",
			from:  0,
			found: false,
		},
		{
			input: " hello ",
			from:  1,
			found: false,
		},
		{
			input: " hello =bye",
			from:  0,
			found: false,
		},
		{
			input:         " hello =bye",
			from:          0,
			allowEmptyKey: true,
			found:         true,
			want:          "",
		},
		{
			input: "hello =bye",
			from:  0,
			found: false,
		},
		{
			input:         "hello =bye",
			from:          0,
			allowEmptyKey: true,
			found:         true,
			want:          "",
		},
		{
			input:         " =bye",
			from:          0,
			allowEmptyKey: true,
			found:         true,
			want:          "",
		},
		{
			input: "=bye",
			from:  0,
			found: false,
		},
		{
			input:         "=bye",
			from:          0,
			allowEmptyKey: true,
			found:         true,
			want:          "",
		},
		{
			input: "",
			from:  0,
			found: false,
		},
		{
			input: "=",
			from:  0,
			found: false,
		},
		{
			input:         "=",
			from:          0,
			allowEmptyKey: true,
			found:         true,
			want:          "",
		},
	}

	for n, tt := range tests {
		t.Logf("test #%d", n)
		start, end, found := findWordFollowedBy('=', []byte(tt.input), tt.from, tt.allowEmptyKey)
		if found != tt.found {
			t.Errorf("want found %v, got %v", tt.found, found)
		}

		if !found {
			continue
		}

		got := string([]byte(tt.input)[start:end])

		if got != tt.want {
			t.Fatalf("want start %q, got %q", tt.want, got)
		}

	}
}

func TestFindUnescaped(t *testing.T) {
	var tests = []struct {
		input    string
		find     rune
		escape   rune
		from     int
		found    bool
		wantRest string
	}{
		{
			input:  "input",
			find:   '"',
			escape: '\\',
			from:   0,
			found:  false,
		},
		{
			input:    `inp"ut`,
			find:     '"',
			escape:   '\\',
			from:     0,
			found:    true,
			wantRest: `"ut`,
		},
		{
			input:  `inp\"ut`,
			find:   '"',
			escape: '\\',
			from:   0,
			found:  false,
		},
		{
			input:    `inp\\"ut`,
			find:     '"',
			escape:   '\\',
			from:     0,
			found:    true,
			wantRest: `"ut`,
		},
		{
			input:  `inp\\\"ut`,
			find:   '"',
			escape: '\\',
			from:   0,
			found:  false,
		},
	}

	for n, tt := range tests {
		t.Logf("test #%d", n)
		idx := findUnescaped(tt.find, tt.escape, []byte(tt.input), tt.from)
		if idx == -1 && tt.found {
			t.Fatalf("should have found %q in %q", tt.wantRest, tt.input)
		}
		if !tt.found {
			continue
		}
		gotRest := string([]byte(tt.input)[idx:])
		if tt.wantRest != gotRest {
			t.Fatalf("want %q, got %q", tt.wantRest, gotRest)
		}
	}
}
