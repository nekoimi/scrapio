module.exports=(schemas,paths,{ref,obj,str,arr,route})=>{
 schemas.TransformSample=obj({value:{},expected:{}});
 schemas.Transform=obj({version:{const:'co.transform.v1'},input:{allOf:[ref('Field'),{properties:{key:{const:'value'}},not:{required:['default']}}]},expression:str({minLength:1,maxLength:8192}),samples:arr(ref('TransformSample'),5,1)});
 schemas.OutputField.properties.transform=ref('Transform');
 schemas.Edge.properties.transforms={type:'object',maxProperties:30,additionalProperties:ref('Transform')};
 schemas.TransformPreviewRequest=obj({transform:ref('Transform'),value:{},target:ref('Field')});
 route('post','/transforms/preview','previewCoTransform','O5','TransformPreviewRequest');
 paths['/transforms/preview'].post['x-implementation-status']='implemented';
 paths['/transforms/preview'].post.description='Pure co.transform.v1 expression on one typed value; 1..5 exact offline samples required. Collector conversion runs after extraction/cleaning before output validation and identity; edge conversion after filtering/expansion/binding before target Schema and allowed-origin checks. Syntax allowlist, no statements/functions/loops/regex/dynamic code/external access; 256 AST nodes/24 depth, intermediate JSON8192bytes/16depth/100array/30keys, runtime25ms/64stack. Before/after, expression fingerprint and error location; no formal writes. Definition/task hashes freeze transforms and samples; changes require new evidence/version. Not a general arbitrary JS runtime or OS memory sandbox.';
};
