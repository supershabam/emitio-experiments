// Generated by BUCKLESCRIPT VERSION 2.2.2, PLEASE EDIT WITH CARE
'use strict';


var domain = (
    function(scale, a, b) {
        return scale.domain([a,b]);
    }
  );

var hex = (
    function(scale, val) {
        return scale(val);
    }
  );

var Scale = /* module */[
  /* domain */domain,
  /* hex */hex
];

exports.Scale = Scale;
/* domain Not a pure module */
