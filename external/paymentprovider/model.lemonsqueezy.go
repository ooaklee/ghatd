package paymentprovider

// LemonSqueezyPricePayload represents the structure of a Lemon Squeezy Price API response
//
// Example Lemon Squeezy Price API Response
//
//	{
//	  "jsonapi": {
//	    "version": "1.0"
//	  },
//	  "links": {
//	    "self": "https://api.lemonsqueezy.com/v1/prices/1"
//	  },
//	  "data": {
//	    "type": "prices",
//	    "id": "1",
//	    "attributes": {
//	      "variant_id": 1,
//	      "category": "subscription",
//	      "scheme": "graduated",
//	      "usage_aggregation": null,
//	      "unit_price": 999,
//	      "unit_price_decimal": null,
//	      "setup_fee_enabled": false,
//	      "setup_fee": null,
//	      "package_size": 1,
//	      "tiers": [
//	        {
//	          "last_unit": 2,
//	          "unit_price": 10000,
//	          "unit_price_decimal": null,
//	          "fixed_fee": 1000
//	        },
//	        {
//	          "last_unit": "inf",
//	          "unit_price": 1000,
//	          "unit_price_decimal": null,
//	          "fixed_fee": 1000
//	        }
//	      ],
//	      "renewal_interval_unit": "year",
//	      "renewal_interval_quantity": 1,
//	      "trial_interval_unit": "day",
//	      "trial_interval_quantity": 30,
//	      "min_price": null,
//	      "suggested_price": null,
//	      "tax_code": "eservice",
//	      "created_at": "2023-05-24T14:15:06.000000Z",
//	      "updated_at": "2023-06-24T14:44:38.000000Z"
//	    },
//	    "relationships": {
//	      "variant": {
//	        "links": {
//	          "related": "https://api.lemonsqueezy.com/v1/prices/1/variant",
//	          "self": "https://api.lemonsqueezy.com/v1/prices/1/relationships/variant"
//	        }
//	      }
//	    },
//	    "links": {
//	      "self": "https://api.lemonsqueezy.com/v1/prices/1"
//	    }
//	  }
//	}
type LemonSqueezyPricePayload struct {
	Data struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		Attributes struct {
			VariantID        int64  `json:"variant_id"`
			Category         string `json:"category"`
			Scheme           string `json:"scheme"`
			UsageAggregation string `json:"usage_aggregation"`
			UnitPrice        int64  `json:"unit_price"`
			UnitPriceDecimal string `json:"unit_price_decimal"`
			SetupFeeEnabled  bool   `json:"setup_fee_enabled"`
			SetupFee         string `json:"setup_fee"`
			PackageSize      int64  `json:"package_size"`
			Tiers            []struct {
				LastUnit         interface{} `json:"last_unit"`
				UnitPrice        int64       `json:"unit_price"`
				UnitPriceDecimal string      `json:"unit_price_decimal"`
				FixedFee         int64       `json:"fixed_fee"`
			} `json:"tiers"`
			RenewalIntervalUnit     string `json:"renewal_interval_unit"`
			RenewalIntervalQuantity int64  `json:"renewal_interval_quantity"`
			TrialIntervalUnit       string `json:"trial_interval_unit"`
			TrialIntervalQuantity   int64  `json:"trial_interval_quantity"`
			MinPrice                string `json:"min_price"`
			SuggestedPrice          string `json:"suggested_price"`
			TaxCode                 string `json:"tax_code"`
			CreatedAt               string `json:"created_at"`
			UpdatedAt               string `json:"updated_at"`
		} `json:"attributes"`
	} `json:"data"`
}

// LemonSqueezyWebhookPayload represents the structure of a Lemon Squeezy webhook payload
type LemonSqueezyWebhookPayload struct {
	Meta struct {
		EventName  string                 `json:"event_name"`
		CustomData map[string]interface{} `json:"custom_data"`
	} `json:"meta"`
	Data struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		Attributes struct {
			StoreID               int64       `json:"store_id"`
			CustomerID            int64       `json:"customer_id"`
			OrderID               int64       `json:"order_id"`
			OrderItemID           int64       `json:"order_item_id"`
			ProductID             int64       `json:"product_id"`
			VariantID             int64       `json:"variant_id"`
			ProductName           string      `json:"product_name"`
			VariantName           string      `json:"variant_name"`
			UserName              string      `json:"user_name"`
			UserEmail             string      `json:"user_email"`
			Status                string      `json:"status"`
			StatusFormatted       string      `json:"status_formatted"`
			CardBrand             string      `json:"card_brand"`
			CardLastFour          string      `json:"card_last_four"`
			Pause                 interface{} `json:"pause"`
			Cancelled             bool        `json:"cancelled"`
			TrialEndsAt           string      `json:"trial_ends_at"`
			BillingAnchor         int64       `json:"billing_anchor"`
			FirstSubscriptionItem struct {
				ID             int64  `json:"id"`
				SubscriptionID int64  `json:"subscription_id"`
				PriceID        int64  `json:"price_id"`
				Quantity       int64  `json:"quantity"`
				CreatedAt      string `json:"created_at"`
				UpdatedAt      string `json:"updated_at"`
			} `json:"first_subscription_item"`
			URLs struct {
				Update string `json:"update_payment_method"`
				Cancel string `json:"customer_portal"`
			} `json:"urls"`
			RenewsAt  string `json:"renews_at"`
			EndsAt    string `json:"ends_at"`
			CreatedAt string `json:"created_at"`
			UpdatedAt string `json:"updated_at"`
		} `json:"attributes"`
	} `json:"data"`
}
