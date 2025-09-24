

-- MySQL schema for EPCs (Ethereum Place Codes)
--


-- Campaign refers to the process / batch / marketing campaign.
-- Multiple campaigns can have the same prefix.
create table if not exists epc_campaigns (
  slug varchar(16) not null,
  key_prefix varchar(255) not null,
  description varchar(255),
  created TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

  PRIMARY KEY (slug)
);

-- Contract refers to an EPC; it has an address derived from a key
create table if not exists epc_contracts (
  id int unsigned AUTO_INCREMENT,

  -- key is: `${epc_campaigns.key_prefix}/${target string}`
  `key` varchar(255) not null unique,
  address varchar(42) not null unique,
  created TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

  PRIMARY KEY (id)
);

-- The template used to send a message (should not be updated)
create table if not exists epc_sent_message_templates_read_only (
  id int unsigned AUTO_INCREMENT,
  body text not null,
  created TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

  PRIMARY KEY (id)
);

-- Sent messages
create table if not exists epc_outbox (
  id int unsigned AUTO_INCREMENT,
  campaign varchar(16) not null,
  contract_id int unsigned not null,
  metadata JSON,
  status ENUM('held', 'pending', 'sent', 'error'),
  sent_message_template_id int unsigned,
  created TIMESTAMP DEFAULT CURRENT_TIMESTAMP,

  PRIMARY KEY (id),
  FOREIGN KEY (campaign) REFERENCES epc_campaigns(slug),
  FOREIGN KEY (contract_id) REFERENCES epc_contracts(id),
  FOREIGN KEY (sent_message_template_id) REFERENCES epc_sent_message_templates_read_only(id)
);
